package opensearch

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"path"
	"strings"
)

func (c *Client) ConcreteIndexUUID(ctx context.Context, index string) (string, error) {
	if index == "" || strings.ContainsAny(index, "*/\\?#%[] ,") || strings.Contains(index, "..") {
		return "", errors.New("verified fields require one concrete index")
	}
	allowed := false
	for _, pattern := range strings.Split(c.config.IndexPattern, ",") {
		matched, err := path.Match(pattern, index)
		if err != nil {
			return "", errors.New("invalid index pattern")
		}
		allowed = allowed || matched
	}
	if !allowed {
		return "", errors.New("index is outside the configured index pattern")
	}
	var resolved struct {
		Indices []struct {
			Name string `json:"name"`
		} `json:"indices"`
		Aliases     []json.RawMessage `json:"aliases"`
		DataStreams []json.RawMessage `json:"data_streams"`
	}
	if err := c.get(ctx, "/_resolve/index/"+url.PathEscape(index), url.Values{"expand_wildcards": {"none"}}, &resolved); err != nil {
		return "", err
	}
	if resolved.Indices == nil || resolved.Aliases == nil || resolved.DataStreams == nil || len(resolved.Indices) != 1 || resolved.Indices[0].Name != index || len(resolved.Aliases) != 0 || len(resolved.DataStreams) != 0 {
		return "", errors.New("verified fields require one concrete index, not an alias or data stream")
	}
	var settings map[string]struct {
		Settings map[string]json.RawMessage `json:"settings"`
	}
	if err := c.get(ctx, "/"+url.PathEscape(index)+"/_settings", url.Values{"flat_settings": {"true"}}, &settings); err != nil {
		return "", err
	}
	entry, ok := settings[index]
	var uuid string
	if len(settings) != 1 || !ok || json.Unmarshal(entry.Settings["index.uuid"], &uuid) != nil || uuid == "" {
		return "", errors.New("index settings did not identify one concrete index UUID")
	}
	return uuid, nil
}
