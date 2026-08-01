package indexer

import "relay/internal/sourceindex/fsatomic"

// exposeNoReplace remains package-local for existing direct indexer tests;
// both staging and supervisor publication use the shared primitive.
func exposeNoReplace(source, target string) error { return fsatomic.RenameNoReplace(source, target) }
