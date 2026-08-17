package contextpack

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	SchemaV1        = "powercontext.prepared-context.v1"
	DefaultMaxBytes = 8_000
	MinMaxBytes     = 512
	MaxMaxBytes     = 32_768
	MaxQueryRunes   = 8_192
)

type Status string

const (
	Empty Status = "empty"
	Ready Status = "ready"
)

type Request struct {
	query    string
	maxBytes int
}

func NewRequest(query string, maxBytes int) (Request, error) {
	request := Request{query: query, maxBytes: maxBytes}
	if request.maxBytes == 0 {
		request.maxBytes = DefaultMaxBytes
	}
	if err := request.Validate(); err != nil {
		return Request{}, err
	}
	return request, nil
}

func (r Request) Validate() error {
	if strings.TrimSpace(r.query) == "" || utf8.RuneCountInString(r.query) > MaxQueryRunes {
		return fmt.Errorf("contextpack: query must contain 1..%d characters", MaxQueryRunes)
	}
	if r.maxBytes < MinMaxBytes || r.maxBytes > MaxMaxBytes {
		return fmt.Errorf("contextpack: max_bytes must be within %d..%d", MinMaxBytes, MaxMaxBytes)
	}
	return nil
}

func (r Request) Query() string { return r.query }
func (r Request) MaxBytes() int { return r.maxBytes }

type Prepared struct {
	status       Status
	content      *string
	contentBytes int
}

func NewPrepared(status Status, content *string, contentBytes int) (Prepared, error) {
	switch status {
	case Empty:
		if content != nil || contentBytes != 0 {
			return Prepared{}, fmt.Errorf("contextpack: empty context must not contain content")
		}
	case Ready:
		if content == nil || strings.TrimSpace(*content) == "" || len([]byte(*content)) != contentBytes {
			return Prepared{}, fmt.Errorf("contextpack: ready context content is inconsistent")
		}
	default:
		return Prepared{}, fmt.Errorf("contextpack: unsupported status %q", status)
	}
	return Prepared{status: status, content: cloneString(content), contentBytes: contentBytes}, nil
}

func EmptyPrepared() Prepared { return Prepared{status: Empty} }

func (p Prepared) Schema() string    { return SchemaV1 }
func (p Prepared) Status() Status    { return p.status }
func (p Prepared) Content() *string  { return cloneString(p.content) }
func (p Prepared) ContentBytes() int { return p.contentBytes }

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
