package extract

type Modification interface {
	Type() string
}

type ModifyFile struct {
	Path        string
	Edits       []Edit
	Explanation string
}

type Edit struct {
	Search  string
	Replace string
}

func (m ModifyFile) Type() string {
	return "ModifyFile"
}

type RemoveFile struct {
	Path        string
	Explanation string
}

func (r RemoveFile) Type() string {
	return "RemoveFile"
}

type CreateFile struct {
	Path        string
	Content     string
	Explanation string
}

func (c CreateFile) Type() string {
	return "CreateFile"
}
