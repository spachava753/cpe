/*
Package render provides terminal-facing markdown and plain-text rendering helpers.

It centralizes TTY detection and renderer construction so higher-level packages
can request appropriately configured renderers without coupling themselves to the
underlying glamour/termenv setup details.
*/
package render
