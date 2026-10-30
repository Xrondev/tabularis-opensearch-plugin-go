package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"tabularis-opensearch-plugin-go/internal/awsdiscovery"
	"tabularis-opensearch-plugin-go/internal/browse"
	"tabularis-opensearch-plugin-go/internal/connection"
	"tabularis-opensearch-plugin-go/internal/connectioncache"
	"tabularis-opensearch-plugin-go/internal/pagination"
	"tabularis-opensearch-plugin-go/internal/ppl"
	"tabularis-opensearch-plugin-go/internal/query"
	"tabularis-opensearch-plugin-go/internal/sqlquery"
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

var errInvalidParams = errors.New("invalid parameters")

type pplRuntime struct {
	once      sync.Once
	mu        sync.Mutex
	validator *ppl.Validator
	err       error
	closed    bool
}

func (r *pplRuntime) get() (*ppl.Validator, error) {
	r.once.Do(func() {
		executable, err := os.Executable()
		var validator *ppl.Validator
		if err == nil {
			validator, err = ppl.Open(filepath.Join(filepath.Dir(executable), "ppl.bundle.js"))
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.closed && validator != nil {
			err = errors.Join(err, validator.Close())
			validator = nil
		}
		r.validator, r.err = validator, err
	})
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, errors.New("PPL validator is closed")
	}
	return r.validator, r.err
}

func (r *pplRuntime) close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	if r.validator != nil {
		return r.validator.Close()
	}
	return nil
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

func serve(input io.Reader, output io.Writer) (err error) {
	cache, err := connectioncache.New()
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, cache.Close()) }()
	pplRuntime := &pplRuntime{}
	defer func() { err = errors.Join(err, pplRuntime.close()) }()
	pages, err := pagination.New(func(error) { fmt.Fprintln(os.Stderr, "PIT cleanup failed") })
	if err != nil {
		return err
	}
	var shutdownDeadline time.Time
	defer func() {
		if shutdownDeadline.IsZero() {
			shutdownDeadline = time.Now().Add(10 * time.Second)
		}
		ctx, cancel := context.WithDeadline(context.Background(), shutdownDeadline)
		defer cancel()
		err = errors.Join(err, pages.CloseContext(ctx))
	}()
	root, cancelRoot := context.WithCancel(context.Background())
	defer cancelRoot()
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), 8*1024*1024)
	encoder := json.NewEncoder(output)
	var writeMu, pendingMu sync.Mutex
	var running sync.WaitGroup
	pending := make(map[string]context.CancelFunc)
	type connectionLimit struct {
		active chan struct{}
		refs   int
	}
	connections := make(map[string]*connectionLimit)
	admitted := make(chan struct{}, 80)
	active := make(chan struct{}, 16)
	outputError := make(chan error, 1)
	type scanResult struct {
		line []byte
		err  error
		done bool
	}
	lines := make(chan scanResult, 1)
	go func() {
		for scanner.Scan() {
			line := append([]byte(nil), scanner.Bytes()...)
			select {
			case lines <- scanResult{line: line}:
			case <-root.Done():
				return
			}
		}
		select {
		case lines <- scanResult{err: scanner.Err(), done: true}:
		case <-root.Done():
		}
	}()
	write := func(resp response) {
		writeMu.Lock()
		err := encoder.Encode(resp)
		writeMu.Unlock()
		if err != nil {
			select {
			case outputError <- err:
			default:
			}
			cancelRoot()
		}
	}
	for {
		var item scanResult
		select {
		case err := <-outputError:
			return err
		case item = <-lines:
		}
		if item.done {
			shutdownDeadline = time.Now().Add(10 * time.Second)
			if item.err != nil {
				cancelRoot()
				return item.err
			}
			done := make(chan struct{})
			go func() { running.Wait(); close(done) }()
			select {
			case <-done:
				select {
				case err := <-outputError:
					return err
				default:
					return nil
				}
			case err := <-outputError:
				return err
			case <-time.After(10 * time.Second):
				cancelRoot()
				return fmt.Errorf("shutdown timed out")
			}
		}
		var req request
		var resp response
		resp.JSONRPC = "2.0"
		resp.ID = json.RawMessage("null")
		if !json.Valid(item.line) {
			resp.Error = &rpcError{Code: -32700, Message: "Parse error"}
		} else if err := json.Unmarshal(item.line, &req); err != nil || req.JSONRPC != "2.0" || req.Method == "" || !validID(req.ID) {
			if req.ID != nil && validID(req.ID) {
				resp.ID = req.ID
			}
			resp.Error = &rpcError{Code: -32600, Message: "Invalid request"}
		} else {
			if req.ID == nil {
				if req.Method == "cancel" {
					var args struct {
						ID json.RawMessage `json:"id"`
					}
					if json.Unmarshal(req.Params, &args) == nil && args.ID != nil {
						pendingMu.Lock()
						cancel := pending[string(args.ID)]
						pendingMu.Unlock()
						if cancel != nil {
							cancel()
						}
					}
				}
				continue
			}
			resp.ID = req.ID
			select {
			case admitted <- struct{}{}:
			default:
				resp.Error = &rpcError{Code: -32603, Message: "Request capacity exceeded"}
				write(resp)
				continue
			}
			var connectionArgs struct {
				Params connection.Params `json:"params"`
			}
			_ = json.Unmarshal(req.Params, &connectionArgs)
			var ctx context.Context
			var cancel context.CancelFunc
			if cfg, err := connection.Parse(connectionArgs.Params); err == nil {
				ctx, cancel = context.WithTimeout(root, cfg.Timeout)
			} else {
				ctx, cancel = context.WithCancel(root)
			}
			key := string(req.ID)
			connectionID := connectionArgs.Params.ConnectionID
			pendingMu.Lock()
			if _, exists := pending[key]; exists {
				pendingMu.Unlock()
				cancel()
				<-admitted
				resp.Error = &rpcError{Code: -32600, Message: "Duplicate request ID"}
				write(resp)
				continue
			}
			pending[key] = cancel
			var limit *connectionLimit
			if connectionID != "" {
				limit = connections[connectionID]
				if limit == nil {
					limit = &connectionLimit{active: make(chan struct{}, 4)}
					connections[connectionID] = limit
				}
				limit.refs++
			}
			pendingMu.Unlock()
			running.Add(1)
			go func(req request, resp response) {
				defer running.Done()
				defer func() {
					pendingMu.Lock()
					delete(pending, key)
					if limit != nil {
						limit.refs--
						if limit.refs == 0 {
							delete(connections, connectionID)
						}
					}
					pendingMu.Unlock()
					cancel()
					<-admitted
				}()
				if limit != nil {
					select {
					case limit.active <- struct{}{}:
						defer func() { <-limit.active }()
					case <-ctx.Done():
						resp.Error = &rpcError{Code: -32603, Message: "Request canceled"}
						write(resp)
						return
					}
				}
				select {
				case active <- struct{}{}:
					defer func() { <-active }()
				case <-ctx.Done():
					resp.Error = &rpcError{Code: -32603, Message: "Request canceled"}
					write(resp)
					return
				}
				dispatch(ctx, cache, pages, pplRuntime, req, &resp)
				write(resp)
			}(req, resp)
			continue
		}
		write(resp)
	}
}

func validID(id json.RawMessage) bool {
	if id == nil || string(id) == "null" {
		return true
	}
	var s string
	if json.Unmarshal(id, &s) == nil {
		return true
	}
	var number json.Number
	return json.Unmarshal(id, &number) == nil && len(id) > 0 && id[0] != '"'
}

func dispatch(ctx context.Context, cache *connectioncache.Cache, pages *pagination.Manager, pplRuntime *pplRuntime, req request, resp *response) {
	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{}
	case "discover_aws_endpoints":
		var options awsdiscovery.Options
		if err := json.Unmarshal(req.Params, &options); err != nil {
			resp.Error = &rpcError{Code: -32602, Message: "Invalid discovery parameters"}
			return
		}
		discoveryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		result, err := awsdiscovery.Discover(discoveryCtx, options)
		if err != nil {
			code := -32603
			var discoveryErr *awsdiscovery.Error
			if errors.As(err, &discoveryErr) && discoveryErr.Kind == "invalid_configuration" {
				code = -32602
			}
			resp.Error = &rpcError{Code: code, Message: err.Error()}
		} else {
			resp.Result = result
		}
	case "get_databases":
		resp.Result = []string{"default"}
	case "get_schemas", "get_foreign_keys", "get_indexes", "get_views", "get_routines":
		resp.Result = []any{}
	case "release_session":
		var args struct {
			SessionID string `json:"session_id"`
		}
		if json.Unmarshal(req.Params, &args) != nil || args.SessionID == "" {
			resp.Error = &rpcError{Code: -32602, Message: "session_id is required"}
		} else if err := pages.Release(args.SessionID); err != nil {
			resp.Error = &rpcError{Code: -32603, Message: err.Error()}
		} else {
			resp.Result = json.RawMessage("null")
		}
	case "test_connection", "ping", "get_tables", "get_columns", "execute_query":
		result, err := handleConnection(ctx, cache, pages, pplRuntime, req.Method, req.Params)
		if err != nil {
			code := -32603
			if errors.Is(err, errInvalidParams) {
				code = -32602
			}
			resp.Error = &rpcError{Code: code, Message: err.Error()}
		} else if req.Method == "ping" {
			resp.Result = json.RawMessage("null")
		} else {
			resp.Result = result
		}
	default:
		resp.Error = &rpcError{Code: -32601, Message: "Method not found"}
	}
}

func handleConnection(parent context.Context, cache *connectioncache.Cache, pages *pagination.Manager, pplRuntime *pplRuntime, method string, raw json.RawMessage) (result any, err error) {
	var args struct {
		Params    connection.Params `json:"params"`
		Table     string            `json:"table"`
		Query     string            `json:"query"`
		Limit     *int              `json:"limit"`
		Page      int               `json:"page"`
		Schema    *string           `json:"schema"`
		SessionID string            `json:"session_id"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("%w: connection parameters", errInvalidParams)
	}
	cfg, err := connection.Parse(args.Params)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errInvalidParams, err)
	}
	ctx, cancel := context.WithTimeout(parent, cfg.Timeout)
	defer cancel()
	var operation query.Request
	var sqlOperation sqlquery.Query
	var pplOperation ppl.Query
	sqlMode := false
	pplMode := false
	browseMode := false
	var browsePlan browse.Plan
	var browsePlanErr error
	pageSize := cfg.PageSize
	if method == "execute_query" {
		if args.Schema != nil && *args.Schema != "" {
			return nil, fmt.Errorf("%w: schemas are not supported", errInvalidParams)
		}
		if args.Limit != nil {
			pageSize = *args.Limit
		}
		if pageSize < 1 || pageSize > 1000 || args.Page < 1 {
			return nil, fmt.Errorf("%w: invalid page or limit", errInvalidParams)
		}
		text := strings.TrimSpace(args.Query)
		mode, body, hasBody := strings.Cut(text, "\n")
		if strings.TrimSpace(mode) == "#!sql" && hasBody {
			sqlMode = true
			sqlOperation, err = sqlquery.ValidatePage(body, cfg.IndexPattern, pageSize, args.Page)
		} else if strings.TrimSpace(mode) == "#!ppl" && hasBody {
			if args.Page != 1 {
				return nil, fmt.Errorf("%w: PPL pagination is not supported", errInvalidParams)
			}
			validator, openErr := pplRuntime.get()
			if openErr != nil {
				return nil, openErr
			}
			pplMode = true
			pplOperation, err = validator.Validate(ctx, body, cfg.IndexPattern, pageSize)
		} else if !strings.HasPrefix(text, "#!") {
			browseMode, err = browse.Candidate(text)
			if err != nil {
				return nil, fmt.Errorf("%w: %v", errInvalidParams, err)
			}
			sqlMode = !browseMode
			if sqlMode {
				sqlOperation, err = sqlquery.ValidatePage(text, cfg.IndexPattern, pageSize, args.Page)
			} else {
				sqlOperation, err = sqlquery.Validate(text, cfg.IndexPattern, pageSize)
			}
		} else {
			page := args.Page
			if cfg.PaginationMode == "pit" {
				page = 1
			}
			operation, err = query.Parse(args.Query, pageSize, page, cfg.IndexPattern)
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %v", errInvalidParams, err)
		}
		if browseMode {
			page := args.Page
			if cfg.PaginationMode == "pit" {
				page = 1
			}
			browsePlan, browsePlanErr = browse.Translate(args.Query, cfg.IndexPattern, pageSize, page, nil, nil)
			if browsePlanErr != nil {
				var evidence *browse.FieldEvidenceError
				assured := false
				if errors.As(browsePlanErr, &evidence) {
					for _, field := range cfg.VerifiedExactFields {
						assured = assured || field == evidence.Field
					}
				}
				if !assured {
					return nil, fmt.Errorf("%w: %v", errInvalidParams, browsePlanErr)
				}
			}
		}
	}
	lease, err := cache.Acquire(ctx, cfg)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, lease.Release()) }()
	client := lease.Client()
	if method == "execute_query" && args.SessionID != "" && (cfg.PaginationMode != "pit" || sqlMode || pplMode || (!browseMode && (operation.Method() != "POST" || !strings.HasSuffix(operation.Path(), "/_search")))) {
		if err := pages.ReleaseOwned(args.SessionID, cfg.ConnectionID); err != nil {
			return nil, err
		}
	}
	switch method {
	case "test_connection":
		if err := client.Test(ctx); err != nil {
			return nil, err
		}
		return map[string]bool{"success": true}, nil
	case "ping":
		return nil, client.Test(ctx)
	case "get_tables":
		return client.Tables(ctx)
	case "get_columns":
		if args.Table == "" {
			return nil, fmt.Errorf("%w: table is required", errInvalidParams)
		}
		return client.Columns(ctx, args.Table)
	case "execute_query":
		if pplMode {
			data, err := client.PPL(ctx, pplOperation)
			if err != nil {
				return nil, err
			}
			return ppl.Normalize(pplOperation, data)
		}
		if browseMode {
			page := args.Page
			if cfg.PaginationMode == "pit" {
				page = 1
			}
			plan, planErr := browsePlan, browsePlanErr
			var indexUUID string
			var evidence *browse.FieldEvidenceError
			verified := make(map[string]bool, len(cfg.VerifiedExactFields))
			for _, field := range cfg.VerifiedExactFields {
				verified[field] = true
			}
			if errors.As(planErr, &evidence) && verified[evidence.Field] {
				if strings.ContainsAny(sqlOperation.Index(), "*?") {
					return nil, fmt.Errorf("%w: verified fields require one concrete index", errInvalidParams)
				}
				indexUUID, err = client.ConcreteIndexUUID(ctx, sqlOperation.Index())
				if err != nil {
					return nil, err
				}
				columns, err := client.Columns(ctx, sqlOperation.Index())
				if err != nil {
					return nil, err
				}
				plan, planErr = browse.Translate(args.Query, cfg.IndexPattern, pageSize, page, columns, verified)
			}
			if planErr != nil {
				return nil, fmt.Errorf("%w: %v", errInvalidParams, planErr)
			}
			verifyIndex := func() error {
				if indexUUID == "" {
					return nil
				}
				current, err := client.ConcreteIndexUUID(ctx, sqlOperation.Index())
				if err != nil {
					return err
				}
				if current != indexUUID {
					return errors.New("index changed during verified browse; retry the query")
				}
				return nil
			}
			if cfg.PaginationMode == "pit" {
				if !plan.PITAllowed() {
					if args.SessionID != "" {
						if err := pages.ReleaseOwned(args.SessionID, cfg.ConnectionID); err != nil {
							return nil, err
						}
					}
					if args.Page != 1 {
						return nil, fmt.Errorf("%w: query has no additional pages", errInvalidParams)
					}
					data, err := client.Do(ctx, plan.Request)
					if err != nil {
						return nil, err
					}
					if err := verifyIndex(); err != nil {
						return nil, err
					}
					return browse.Normalize(plan, data)
				}
				identity := args.Query
				if indexUUID != "" {
					identity += "\x00" + indexUUID
				}
				data, err := pages.VerifiedPage(ctx, lease, cfg.ConnectionID, args.SessionID, plan.Request, args.Page, identity)
				if err != nil {
					return nil, err
				}
				if err := verifyIndex(); err != nil {
					return nil, errors.Join(err, pages.ReleaseOwned(args.SessionID, cfg.ConnectionID))
				}
				return browse.NormalizePIT(plan, data, args.Page)
			}
			data, err := client.Do(ctx, plan.Request)
			if err != nil {
				return nil, err
			}
			if err := verifyIndex(); err != nil {
				return nil, err
			}
			return browse.Normalize(plan, data)
		}
		if sqlMode {
			data, err := client.SQL(ctx, sqlOperation)
			if err != nil {
				return nil, err
			}
			return sqlquery.Normalize(sqlOperation, data)
		}
		if cfg.PaginationMode == "pit" && operation.Method() == "POST" && strings.HasSuffix(operation.Path(), "/_search") {
			var body struct {
				Size int `json:"size"`
			}
			if err := json.Unmarshal(operation.Body(), &body); err != nil {
				return nil, fmt.Errorf("%w: invalid search body", errInvalidParams)
			}
			if body.Size == 0 {
				if args.SessionID != "" {
					if err := pages.ReleaseOwned(args.SessionID, cfg.ConnectionID); err != nil {
						return nil, err
					}
				}
				if args.Page != 1 {
					return nil, fmt.Errorf("%w: aggregation results have no pages", errInvalidParams)
				}
				data, err := client.Do(ctx, operation)
				if err != nil {
					return nil, err
				}
				return query.Normalize(operation, data)
			}
			data, err := pages.VerifiedPage(ctx, lease, cfg.ConnectionID, args.SessionID, operation, args.Page, args.Query)
			if err != nil {
				return nil, err
			}
			result, err := query.Normalize(operation, data)
			if err == nil && result.Pagination != nil {
				result.Pagination.Page = args.Page
			}
			return result, err
		}
		if cfg.PaginationMode == "pit" && args.Page != 1 {
			return nil, fmt.Errorf("%w: inspection results have no additional pages", errInvalidParams)
		}
		data, err := client.Do(ctx, operation)
		if err != nil {
			return nil, err
		}
		return query.Normalize(operation, data)
	default:
		return nil, fmt.Errorf("unsupported method")
	}
}

func main() {
	if err := serve(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
