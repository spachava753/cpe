package tiktokenloader

import (
	_ "embed"
	"encoding/base64"
	"fmt"
	"path"
	"strconv"
	"strings"
)

//go:embed o200k_base.tiktoken
var o200kBaseDict []byte

const (
	O200kBaseDictFile = "o200k_base.tiktoken"
)

type OfflineO200kBaseDictLoader struct{}

func (l *OfflineO200kBaseDictLoader) LoadTiktokenBpe(tiktokenBpeFile string) (map[string]int, error) {
	baseFileName := path.Base(tiktokenBpeFile)
	if baseFileName != O200kBaseDictFile {
		return nil, fmt.Errorf("unknown tiktoken bpe file: %s", tiktokenBpeFile)
	}
	bpeRanks := make(map[string]int)
	for _, line := range strings.Split(string(o200kBaseDict), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, " ")
		token, err := base64.StdEncoding.DecodeString(parts[0])
		if err != nil {
			return nil, err
		}
		rank, err := strconv.Atoi(parts[1])
		if err != nil {
			return nil, err
		}
		bpeRanks[string(token)] = rank
	}
	return bpeRanks, nil
}

func NewOfflineLoader() *OfflineO200kBaseDictLoader {
	return &OfflineO200kBaseDictLoader{}
}
