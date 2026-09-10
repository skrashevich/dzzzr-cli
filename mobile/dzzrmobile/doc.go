// Package dzzrmobile provides gomobile-compatible bindings for the dzzzr
// client, so an iOS or Android app can play Dozor Classic through the same
// code the CLI uses.
//
// gomobile can only carry a narrow set of types across the language boundary,
// so every method here returns a JSON string that the app decodes into its own
// model, every integer is int64, and nothing takes a context: cancellation and
// timeouts are configured on the client instead.
//
//	gomobile bind -target=ios     -o dzzzr.xcframework ./mobile/dzzrmobile
//	gomobile bind -target=android -o dzzzr.aar         ./mobile/dzzrmobile
package dzzrmobile
