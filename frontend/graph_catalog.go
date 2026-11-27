package frontend

import _ "embed"

//go:embed src/graph/kinds.json
var graphKinds []byte

// GraphKinds returns the subject kind catalog shared by the engine, the
// desktop UI and the phone. A caller cannot mutate the embedded copy.
func GraphKinds() []byte { return append([]byte(nil), graphKinds...) }
