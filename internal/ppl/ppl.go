package ppl

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/dop251/goja"
)

const bundleHash = "636c872a8bfdccd498322214c21e93cd9fa995d8ffb29b16092b5cd96cac8e27"
const maxBundleBytes = 2 * 1024 * 1024
const maxInputBytes = 32768

type Query struct {
	text  string
	index string
	size  int
}

func (q Query) Text() string  { return q.text }
func (q Query) Index() string { return q.index }
func (q Query) PageSize() int { return q.size }

func (q Query) Validate() error {
	if q.text == "" || q.index == "" || q.size < 1 || q.size > 1000 {
		return errors.New("invalid PPL query")
	}
	return nil
}

type worker struct {
	runtime *goja.Runtime
	parse   goja.Callable
}

type Validator struct {
	program *goja.Program
	slots   chan struct{}
	idle    chan *worker
	done    chan struct{}
	once    sync.Once
}

func Open(assetPath string) (*Validator, error) {
	file, err := os.Open(assetPath)
	if err != nil {
		return nil, errors.New("PPL parser artifact is unavailable")
	}
	data, readErr := io.ReadAll(io.LimitReader(file, maxBundleBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(data) > maxBundleBytes {
		return nil, errors.New("PPL parser artifact cannot be read or exceeds its size limit")
	}
	digest := sha256.Sum256(data)
	if fmt.Sprintf("%x", digest) != bundleHash {
		return nil, errors.New("PPL parser artifact integrity check failed")
	}
	program, err := goja.Compile("ppl.bundle.js", string(data), true)
	if err != nil {
		return nil, errors.New("PPL parser artifact cannot be compiled")
	}
	return &Validator{program: program, slots: make(chan struct{}, 2), idle: make(chan *worker, 2), done: make(chan struct{})}, nil
}

func (v *Validator) Close() error {
	v.once.Do(func() { close(v.done) })
	return nil
}

func (v *Validator) Validate(ctx context.Context, text, scope string, pageSize int) (Query, error) {
	if err := ctx.Err(); err != nil {
		return Query{}, err
	}
	if !utf8.ValidString(text) || len(text) == 0 || len(text) > maxInputBytes-32 || pageSize < 1 || pageSize > 1000 || scope == "" {
		return Query{}, errors.New("invalid PPL query, index scope, or page size")
	}
	select {
	case <-ctx.Done():
		return Query{}, ctx.Err()
	case <-v.done:
		return Query{}, errors.New("PPL validator is closed")
	case v.slots <- struct{}{}:
	}
	defer func() { <-v.slots }()
	select {
	case <-v.done:
		return Query{}, errors.New("PPL validator is closed")
	default:
	}
	var current *worker
	select {
	case current = <-v.idle:
	default:
		current = &worker{runtime: goja.New()}
		current.runtime.SetMaxCallStackSize(1024)
	}
	keep := false
	defer func() {
		if keep {
			select {
			case v.idle <- current:
			default:
			}
		}
	}()
	interruptDone := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		current.runtime.Interrupt(ctx.Err())
		close(interruptDone)
	})
	defer func() {
		if !stop() {
			<-interruptDone
			keep = false
		}
	}()
	if current.parse == nil {
		if _, err := current.runtime.RunProgram(v.program); err != nil {
			if ctx.Err() != nil {
				return Query{}, ctx.Err()
			}
			return Query{}, errors.New("PPL parser initialization failed")
		}
		api := current.runtime.Get("PPLParser")
		if api == nil || goja.IsUndefined(api) || goja.IsNull(api) {
			return Query{}, errors.New("PPL parser entry point is unavailable")
		}
		var ok bool
		current.parse, ok = goja.AssertFunction(api.ToObject(current.runtime).Get("parse"))
		if !ok {
			return Query{}, errors.New("PPL parser entry point is invalid")
		}
	}
	keep = true
	parse := func(input string) (treeInfo, error) {
		value, err := current.parse(goja.Undefined(), current.runtime.ToValue(input))
		if err != nil {
			keep = false
			if ctx.Err() != nil {
				return treeInfo{}, ctx.Err()
			}
			return treeInfo{}, errors.New("PPL parser execution failed")
		}
		encoded := value.String()
		if len(encoded) > 2*1024*1024 {
			return treeInfo{}, errors.New("PPL parse tree exceeds its resource limit")
		}
		var result parsedTree
		if err := json.Unmarshal([]byte(encoded), &result); err != nil || result.Error != "" {
			return treeInfo{}, errors.New("PPL query syntax or resource limit is invalid")
		}
		return validateTree(result.Root, scope, pageSize+1)
	}
	info, err := parse(text)
	if err != nil {
		return Query{}, err
	}
	bounded := strings.TrimSpace(text)
	if info.hasHead {
		if info.head > pageSize+1 {
			return Query{}, errors.New("PPL head exceeds the supported result limit")
		}
	} else {
		bounded += " | head " + strconv.Itoa(pageSize+1)
		boundedInfo, err := parse(bounded)
		if err != nil {
			return Query{}, err
		}
		if info.index != boundedInfo.index || !boundedInfo.hasHead || boundedInfo.head != pageSize+1 {
			return Query{}, errors.New("PPL source changed while applying the result limit")
		}
	}
	if err := ctx.Err(); err != nil {
		keep = false
		return Query{}, err
	}
	return Query{text: bounded, index: info.index, size: pageSize}, nil
}
