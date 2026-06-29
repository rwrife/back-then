// Package rank scores candidate files for a query by blending time proximity
// to the target window, signal richness (how much metadata we have), and
// folder cohesion (files from the same burst/folder reinforce each other).
//
// Basic ranking arrives in M3 (find); the blended scoring is refined in M5.
package rank
