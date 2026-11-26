// Package cli provides reusable command-line presentation helpers.
//
// Commands print a banner when work begins, use PhaseTracker for aggregate
// progress phases, and finish with a Summary. Use Complete for short phases
// that need one finished line; use Start and End when users need to see a
// longer operation begin. Keep per-resource diagnostic details at debug level.
package cli
