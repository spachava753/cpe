// Package pathcollate provides pinned English/root Unicode ordering for skill hashes.
// It implements tertiary, non-ignorable ICU 78.3 collation, not locale tailoring.
package pathcollate

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"slices"
	"sync"

	"golang.org/x/text/unicode/norm"
)

type mapping struct {
	prefix  []rune
	text    []rune
	weights []uint64
}

type table struct {
	mappings map[rune][]mapping
	han      map[rune]uint32
}

var rootTable = sync.OnceValue(loadTable)

// Key returns a byte-comparable key using ICU 78.3 English/root ordering.
// Keys use Unicode 17 data and do not consult the host locale.
func Key(s string) []byte {
	t := rootTable()
	runes := decompose(s)
	var primary, secondary, tertiary []byte
	appendWeights := func(weights []uint64) {
		for _, weight := range weights {
			if p := uint32(weight >> 32); p != 0 {
				primary = binary.BigEndian.AppendUint32(
					primary,
					p,
				)
			}
			if s := uint16(weight >> 16); s != 0 {
				secondary = binary.BigEndian.AppendUint16(
					secondary,
					s,
				)
			}
			// ICU's default caseFirst=off compares tertiary weights without separate case bits.
			if v := uint16(weight) & 0x3f3f; v != 0 {
				tertiary = binary.BigEndian.AppendUint16(
					tertiary,
					v,
				)
			}
		}
	}
	consumed := make(
		[]bool,
		len(runes),
	)
	for i := range runes {
		if consumed[i] {
			continue
		}
		weights, suffix := t.match(
			runes,
			i,
			consumed,
		)
		appendWeights(weights)
		consumed[i] = true
		for _, position := range suffix {
			consumed[position] = true
		}
		// Skipped marks remain in the input. Their next matching pass can form
		// another contraction, including with later unconsumed characters.
	}
	key := append(
		primary,
		0,
	)
	key = append(
		key,
		secondary...,
	)
	key = append(
		key,
		0,
	)
	return append(
		key,
		tertiary...,
	)
}

// Decompose each code point separately, then canonically reorder its marks.
// norm.NFD.String on the whole path would insert stream-safe CGJs after 30 marks,
// which introduces boundaries that ICU does not add.
func decompose(s string) []rune {
	var runes []rune
	var classes []uint8
	for _, r := range s {
		// ICU's reserved Hangul script boundary matches the original syllable,
		// not the NFD prefix of every syllable with the same leading Jamo.
		if r == 0xac00 && len(runes) > 0 && runes[len(runes)-1] == 0xfdd1 {
			runes[len(runes)-1] = 0x110000
			continue
		}
		for _, decomposed := range norm.NFD.String(string(r)) {
			ccc := norm.NFD.PropertiesString(string(decomposed)).CCC()
			runes = append(
				runes,
				decomposed,
			)
			classes = append(
				classes,
				ccc,
			)
			for i := len(runes) - 1; ccc != 0 && i > 0 && classes[i-1] > ccc; i-- {
				runes[i], runes[i-1] = runes[i-1], runes[i]
				classes[i], classes[i-1] = classes[i-1], classes[i]
			}
		}
	}
	return runes
}

func (t table) match(text []rune, start int, consumed []bool) ([]uint64, []int) {
	weights := t.single(text[start])
	matchedText := []rune{
		text[start],
	}
	prefixLength := -1
	var suffix []int
	prefixMatches := func(prefix []rune) bool {
		return len(prefix) <= start && slices.Equal(
			prefix,
			text[start-len(prefix):start],
		)
	}

	// UCA S2.1: find the longest complete contiguous match first.
	for _, candidate := range t.mappings[text[start]] {
		if !prefixMatches(candidate.prefix) {
			continue
		}
		position := start + 1
		var positions []int
		matched := true
		for _, wanted := range candidate.text[1:] {
			for position < len(text) && consumed[position] {
				position++
			}
			if position == len(text) || text[position] != wanted {
				matched = false
				break
			}
			positions = append(
				positions,
				position,
			)
			position++
		}
		if matched && (len(candidate.text) > len(matchedText) || len(candidate.text) == len(matchedText) && len(candidate.prefix) > prefixLength) {
			weights, matchedText, suffix, prefixLength = candidate.weights, candidate.text, positions, len(candidate.prefix)
		}
	}
	if prefixLength < 0 {
		return weights, suffix // A default implicit weight is not a match in the table.
	}

	// UCA S2.1.1–3: extend that match one unblocked non-starter at a time.
	position := start + 1
	if len(suffix) != 0 {
		position = suffix[len(suffix)-1] + 1
	}
	var blocked uint8
	for ; position < len(text); position++ {
		if consumed[position] {
			continue
		}
		r := text[position]
		ccc := norm.NFD.PropertiesString(string(r)).CCC()
		if ccc == 0 {
			break
		}
		var extension *mapping
		if blocked < ccc {
			for _, candidate := range t.mappings[text[start]] {
				if len(candidate.text) == len(matchedText)+1 && candidate.text[len(matchedText)] == r && slices.Equal(
					candidate.text[:len(matchedText)],
					matchedText,
				) && prefixMatches(candidate.prefix) {
					if extension == nil || len(candidate.prefix) > len(extension.prefix) {
						extension = &candidate
					}
				}
			}
		}
		if extension == nil {
			blocked = ccc
			continue
		}
		weights, matchedText = extension.weights, extension.text
		suffix = append(
			suffix,
			position,
		)
	}
	return weights, suffix
}

func (t table) single(r rune) []uint64 {
	for _, entry := range t.mappings[r] {
		if len(entry.text) == 1 && len(entry.prefix) == 0 {
			return entry.weights
		}
	}
	if primary, ok := t.han[r]; ok {
		return []uint64{
			uint64(primary)<<32 | 0x05000500,
		}
	}
	var primary uint32
	switch r {
	case 0xfffd:
		primary = 0xfffd0000
	case 0xfffe:
		primary = 0x02000000
	case 0xffff:
		primary = 0xffff0000
	default:
		// ICU's unassigned-primary algorithm covers all code points, including noncharacters.
		c := uint32(r) + 1
		primary = 2 + c%18*14
		c /= 18
		primary |= (2 + c%254) << 8
		c /= 254
		primary |= (4+c%251)<<16 | 0xfe000000
	}
	return []uint64{
		uint64(primary)<<32 | 0x05000500,
	}
}

func loadTable() table {
	encoded, err := base64.StdEncoding.DecodeString(collationData)
	if err != nil {
		panic(fmt.Sprintf(
			"invalid generated collation data: %v",
			err,
		))
	}
	reader, err := gzip.NewReader(bytes.NewReader(encoded))
	if err != nil {
		panic(fmt.Sprintf(
			"invalid generated collation data: %v",
			err,
		))
	}
	data, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil {
		panic(fmt.Sprintf(
			"invalid generated collation data: %v, %v",
			readErr,
			closeErr,
		))
	}
	r := bytes.NewReader(data)
	number := func() uint64 {
		n, err := binary.ReadUvarint(r)
		if err != nil {
			panic(fmt.Sprintf(
				"invalid generated collation data: %v",
				err,
			))
		}
		return n
	}
	t := table{
		mappings: make(map[rune][]mapping),
		han:      make(map[rune]uint32),
	}
	count := number()
	lead := uint32(0xfe - (count*2+4+254*254-1)/(254*254))
	for i := range count {
		offset := uint32(i) * 2
		primary := (lead+offset/(254*254))<<24 | (2+offset/254%254)<<16 | (2+offset%254)<<8
		t.han[rune(number())] = primary
	}
	count = number()
	for range count {
		entry := mapping{}
		for _, destination := range []*[]rune{
			&entry.prefix,
			&entry.text,
		} {
			length := number()
			for range length {
				*destination = append(
					*destination,
					rune(number()),
				)
			}
		}
		length := number()
		for range length {
			var weight uint64
			if err := binary.Read(
				r,
				binary.BigEndian,
				&weight,
			); err != nil {
				panic(fmt.Sprintf(
					"invalid generated collation data: %v",
					err,
				))
			}
			entry.weights = append(
				entry.weights,
				weight,
			)
		}
		first := entry.text[0]
		t.mappings[first] = append(
			t.mappings[first],
			entry,
		)
	}
	if r.Len() != 0 {
		panic("invalid generated collation data: trailing bytes")
	}
	return t
}
