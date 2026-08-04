package note

import "encoding/json"

// FlattenDocForTest exposes the document walker to the test suite, which lives
// in package note_test and can't reach the unexported function.
func FlattenDocForTest(doc json.RawMessage) string { return flattenDoc(doc) }
