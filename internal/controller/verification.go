package controller

import "bop/internal/core"

// resolveVerification implements the documented verification decision: a
// per-host override, when present, replaces the global default wholesale
// (block-level), not merged field by field. A partially specified override
// (e.g. enabled: true with target_dir omitted) is used exactly as given,
// not backfilled from the global default - specify it fully, or not at
// all. This avoids a footgun: core.Verification.Enabled is a bare bool, so
// there is no way to distinguish "explicitly false" from "omitted" within
// a present override block, which rules out field-level merging.
func resolveVerification(global core.Verification, override *core.Verification) core.Verification {
	if override != nil {
		return *override
	}
	return global
}
