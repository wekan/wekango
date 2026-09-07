package compatibility

import _ "embed"

// Manifest records source provenance and outstanding contracts. It is not a
// claim that every inventoried setting or route is implemented.
//
//go:embed source-manifest.json
var Manifest []byte
