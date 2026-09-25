// Package jsonconfig provides strict decoding shared by application and theme
// configuration. It rejects duplicate keys at every nesting level instead of
// silently choosing one value. Struct fields use their exact JSON spelling;
// case aliases are rejected, while map keys remain case-sensitive domain names. Each file must contain exactly one JSON object.
package jsonconfig
