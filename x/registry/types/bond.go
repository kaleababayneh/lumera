
// Package types defines bond-related helper types for the registry module.
package types

// Intentionally empty. The concrete bond types (BondRecord,
// BondSnapshot, SlashEvent) are proto-generated into types.pb.go;
// SlashRestitutionSplit lives in x/registry/keeper/bond.go because
// module-account routing is a keeper concern. This file is the
// package-documentation anchor for "where does bond type wiring
// live?" — add handwritten helpers here only if they need to be
// usable from places that import types/ without pulling the keeper.
