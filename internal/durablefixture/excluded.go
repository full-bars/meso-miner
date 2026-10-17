//go:build !(linux || darwin)

// The durable fixture provisions linux/darwin ownership and snapshot
// declarations through the real kernel census; other platforms carry no
// files so cross-target vet and build see an empty package, matching the
// fixture's exclusion by design.
package durablefixture
