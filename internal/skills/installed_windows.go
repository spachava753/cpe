package skills

import (
	"os"
	"unsafe"

	"github.com/spf13/afero"
	"golang.org/x/sys/windows"
)

const (
	installedOpenFlags   = 0
	installedParentFlags = 0
)

func openInstalledNative(parent uintptr, name string, filename string, _ int) (afero.File, error) {
	// NT relative opens use an empty name to reopen the parent directory.
	if name == "." {
		name = ""
	}
	objectName, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return nil, err
	}
	attributes := windows.OBJECT_ATTRIBUTES{
		RootDirectory: windows.Handle(parent),
		ObjectName:    objectName,
		Attributes:    windows.OBJ_CASE_INSENSITIVE | windows.OBJ_DONT_REPARSE,
	}
	attributes.Length = uint32(unsafe.Sizeof(attributes))
	var handle windows.Handle
	var status windows.IO_STATUS_BLOCK
	err = windows.NtCreateFile(
		&handle,
		windows.FILE_GENERIC_READ,
		&attributes,
		&status,
		nil,
		0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		windows.FILE_OPEN,
		windows.FILE_OPEN_REPARSE_POINT|windows.FILE_SYNCHRONOUS_IO_NONALERT,
		0,
		0,
	)
	if err != nil {
		return nil, err
	}
	return os.NewFile(
		uintptr(handle),
		filename,
	), nil
}
