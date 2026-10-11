# Skill-hash filename ordering

The upstream skills CLI sorts relative filenames with JavaScript `localeCompare`
before hashing each original filename and its contents. Go's `x/text/collate`
still uses Unicode 6.2 collation tables, so it gives different hashes for modern
emoji and other Unicode filenames.

CPE implements the needed ordering in Go with ICU 78.3 English/root data
(Unicode 17). The comparator uses primary, secondary, and tertiary weights,
non-ignorable punctuation, and the default case ordering. It handles expansions,
contextual and discontiguous contractions, canonical decomposition, Han
radical/stroke ordering, and implicit weights for unassigned characters. It does
not implement other locale tailorings, numeric sorting, or configurable collation.
The original filename bytes, not normalized text or sort keys, enter the hash.

This pin matches `localeCompare` in Node 26.8.2 / ICU 78.3 with the `en-US`
locale. Upstream does not pin its locale or ICU version, so matching every
upstream environment is not possible with one fixed comparator. CPE does not
consult the host locale, call an executable, link system ICU, or download data
at runtime.

## Data and verification

`tables.go` contains compressed data derived from
[ICU 78.3 FractionalUCA.txt](https://github.com/unicode-org/icu/blob/release-78.3/icu4c/source/data/unidata/FractionalUCA.txt).
The data is covered by the [Unicode License V3](LICENSE); release archives include
it as `licenses/unicode/LICENSE`.

Regenerate the checked-in table with:

```sh
go run ./build collation-data
```

This developer task downloads only the pinned source, verifies its SHA-256,
and produces deterministic Go source. The runtime decompresses that embedded
table once. Normalization uses `x/text/unicode/norm`'s Unicode 17 data; update
and verify both data versions together if the collation pin changes.

The Go tests include upstream-generated folder hashes, named comparison vectors,
and reference digests for 208,174 scalar values and 10,000 mixed-script paths.
They cover modern emoji, case and compatibility weights, canonical-equivalence
ties, long combining sequences, contractions, Han, and noncharacter boundaries.
These expected results were generated with Node 26.8.2 / ICU 78.3 and `en-US`;
Node is not needed to run the tests.
