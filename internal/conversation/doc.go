// Package conversation owns saved transcripts, checkpoint storage, compaction
// boundaries, and token accounting. Records contain no live runtimes or protocol
// state; restoring a record never replays its tool calls.
package conversation
