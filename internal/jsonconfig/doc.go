// Package jsonconfig provides strict decoding shared by application and theme
// configuration. It rejects duplicate keys at every nesting level instead of
// silently choosing one value. Each file must contain exactly one JSON object.
package jsonconfig
