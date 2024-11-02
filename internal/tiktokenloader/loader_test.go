package tiktokenloader

import (
	"testing"
)

func TestOfflineO200kBaseDictLoader(t *testing.T) {
	loader := NewOfflineLoader()
	bpeRanks, err := loader.LoadTiktokenBpe("https://openaipublic.blob.core.windows.net/encodings/o200k_base.tiktoken")
	if err != nil {
		t.Error(err)
	}
	if len(bpeRanks) == 0 {
		t.Error("bpeRanks is empty")
	}
}
