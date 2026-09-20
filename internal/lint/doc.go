// Package lint holds the repo-wide hygiene checks that no single package
// can own, expressed as ordinary Go tests over the source tree.
//
// It has no runtime code and nothing imports it: the checks live in the
// package's _test.go files and run with the rest of the suite under
// `make precommit`.
// Put a rule here when it is about how the whole tree is written rather
// than about what one package computes.
package lint
