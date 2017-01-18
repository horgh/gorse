// Package gorselib provides helper function for interacting with RSS, RDF,
// and Atom feeds. Primarily this surrounds building and reading/parsing.
package gorselib

// Config controls package wide settings.
type Config struct {
	// Control whether we have verbose output (or not).
	Quiet bool
}

// Use a global default set of settings.
//
// See package log for a similar approach (global default settings).
var config = Config{
	Quiet: false,
}

// SetQuiet controls the gorselib setting 'Quiet'.
func SetQuiet(quiet bool) {
	config.Quiet = quiet
}
