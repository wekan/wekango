// Package yaml preserves the historical import path while delegating all
// parsing and encoding to the security-maintained YAML organization module.
// Aliases preserve interface and concrete-type identity between callers using
// either path. No archived parser implementation is included here.
package yaml

import (
	maintained "go.yaml.in/yaml/v2"
	"io"
)

type MapSlice = maintained.MapSlice
type MapItem = maintained.MapItem
type Unmarshaler = maintained.Unmarshaler
type Marshaler = maintained.Marshaler
type Decoder = maintained.Decoder
type Encoder = maintained.Encoder
type TypeError = maintained.TypeError
type IsZeroer = maintained.IsZeroer

func Unmarshal(in []byte, out any) error       { return maintained.Unmarshal(in, out) }
func UnmarshalStrict(in []byte, out any) error { return maintained.UnmarshalStrict(in, out) }
func Marshal(in any) ([]byte, error)           { return maintained.Marshal(in) }
func NewDecoder(r io.Reader) *Decoder          { return maintained.NewDecoder(r) }
func NewEncoder(w io.Writer) *Encoder          { return maintained.NewEncoder(w) }
func FutureLineWrap()                          { maintained.FutureLineWrap() }
