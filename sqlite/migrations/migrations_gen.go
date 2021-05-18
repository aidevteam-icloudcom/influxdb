// Code generated by go-bindata. DO NOT EDIT.
// sources:
// 0001_create_notebooks_table.sql (377B)

package migrations

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func bindataRead(data []byte, name string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewBuffer(data))
	if err != nil {
		return nil, fmt.Errorf("Read %q: %v", name, err)
	}

	var buf bytes.Buffer
	_, err = io.Copy(&buf, gz)
	clErr := gz.Close()

	if err != nil {
		return nil, fmt.Errorf("Read %q: %v", name, err)
	}
	if clErr != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

type asset struct {
	bytes  []byte
	info   os.FileInfo
	digest [sha256.Size]byte
}

type bindataFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
}

func (fi bindataFileInfo) Name() string {
	return fi.name
}
func (fi bindataFileInfo) Size() int64 {
	return fi.size
}
func (fi bindataFileInfo) Mode() os.FileMode {
	return fi.mode
}
func (fi bindataFileInfo) ModTime() time.Time {
	return fi.modTime
}
func (fi bindataFileInfo) IsDir() bool {
	return false
}
func (fi bindataFileInfo) Sys() interface{} {
	return nil
}

var __0001_create_notebooks_tableSql = []byte("\x1f\x8b\x08\x00\x00\x00\x00\x00\x00\xff\x64\x8f\xc1\x6a\xeb\x30\x10\x45\xf7\xfa\x8a\x8b\x57\xef\x41\x0d\xca\xb6\xa1\x0b\x35\x88\x12\x6a\xbb\xc6\x55\x21\x59\x09\xc5\x1e\xd7\xa2\xb6\x15\x24\x39\xf4\xf3\x8b\x1d\xea\x12\xb2\x9c\xc3\xcc\xb9\x73\xd3\x14\xaa\x23\x4c\x81\xbc\xbe\x90\x0f\xd6\x8d\x08\x9d\x9b\xfa\x06\x83\x89\x75\x87\xd8\x11\x12\xce\xf9\x21\x41\xeb\xdd\xb0\xcc\xad\xed\x09\xa3\x19\x88\xa5\x29\xe4\xf7\x23\x38\xe7\x1b\x5d\x7b\x32\x91\xf4\xe8\x22\x9d\x9c\xfb\x0a\x3a\x9a\x53\x4f\xbf\xb6\xce\x5c\x08\x66\x0d\xb2\xc1\x8d\x70\x2d\x36\xac\xac\xc4\x4b\x2e\x6e\x3e\x78\xda\x6c\xd9\xac\xde\x2d\xc6\x25\xd2\x8e\x36\x5a\xd3\xe3\xea\x8c\x0e\x21\x3a\x4f\x58\xc3\xd8\xae\x92\x42\x49\x28\xf1\x9c\x49\x24\x2b\x4f\xf0\x8f\x01\xb6\x81\x92\x07\x85\xe2\x4d\xa1\xf8\xc8\x32\x94\xd5\x3e\x17\xd5\x11\xaf\xf2\xf8\xc0\x00\xe7\x3f\xb5\x6d\xb0\x2f\xfe\x56\x66\x3c\x57\xbc\x3d\x9c\x69\x38\x53\x7d\x4f\xaf\xed\x1b\x6d\x22\xd4\x3e\x97\xef\x4a\xe4\xe5\xcc\xa7\x73\x73\xc7\xd9\xff\x2d\xfb\x09\x00\x00\xff\xff\xfa\x0d\x5b\xbc\x79\x01\x00\x00")

func _0001_create_notebooks_tableSqlBytes() ([]byte, error) {
	return bindataRead(
		__0001_create_notebooks_tableSql,
		"0001_create_notebooks_table.sql",
	)
}

func _0001_create_notebooks_tableSql() (*asset, error) {
	bytes, err := _0001_create_notebooks_tableSqlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "0001_create_notebooks_table.sql", size: 377, mode: os.FileMode(420), modTime: time.Unix(1621864389, 0)}
	a := &asset{bytes: bytes, info: info, digest: [32]uint8{0x4d, 0x3e, 0x28, 0x95, 0x61, 0xa2, 0x25, 0x32, 0xb3, 0x33, 0x8, 0x21, 0xe6, 0xf5, 0xcd, 0xb7, 0x26, 0x1d, 0xec, 0x29, 0x6b, 0x53, 0x87, 0x69, 0x1, 0x39, 0x16, 0x3b, 0xf, 0x2c, 0xed, 0xb7}}
	return a, nil
}

// Asset loads and returns the asset for the given name.
// It returns an error if the asset could not be found or
// could not be loaded.
func Asset(name string) ([]byte, error) {
	canonicalName := strings.Replace(name, "\\", "/", -1)
	if f, ok := _bindata[canonicalName]; ok {
		a, err := f()
		if err != nil {
			return nil, fmt.Errorf("Asset %s can't read by error: %v", name, err)
		}
		return a.bytes, nil
	}
	return nil, fmt.Errorf("Asset %s not found", name)
}

// AssetString returns the asset contents as a string (instead of a []byte).
func AssetString(name string) (string, error) {
	data, err := Asset(name)
	return string(data), err
}

// MustAsset is like Asset but panics when Asset would return an error.
// It simplifies safe initialization of global variables.
func MustAsset(name string) []byte {
	a, err := Asset(name)
	if err != nil {
		panic("asset: Asset(" + name + "): " + err.Error())
	}

	return a
}

// MustAssetString is like AssetString but panics when Asset would return an
// error. It simplifies safe initialization of global variables.
func MustAssetString(name string) string {
	return string(MustAsset(name))
}

// AssetInfo loads and returns the asset info for the given name.
// It returns an error if the asset could not be found or
// could not be loaded.
func AssetInfo(name string) (os.FileInfo, error) {
	canonicalName := strings.Replace(name, "\\", "/", -1)
	if f, ok := _bindata[canonicalName]; ok {
		a, err := f()
		if err != nil {
			return nil, fmt.Errorf("AssetInfo %s can't read by error: %v", name, err)
		}
		return a.info, nil
	}
	return nil, fmt.Errorf("AssetInfo %s not found", name)
}

// AssetDigest returns the digest of the file with the given name. It returns an
// error if the asset could not be found or the digest could not be loaded.
func AssetDigest(name string) ([sha256.Size]byte, error) {
	canonicalName := strings.Replace(name, "\\", "/", -1)
	if f, ok := _bindata[canonicalName]; ok {
		a, err := f()
		if err != nil {
			return [sha256.Size]byte{}, fmt.Errorf("AssetDigest %s can't read by error: %v", name, err)
		}
		return a.digest, nil
	}
	return [sha256.Size]byte{}, fmt.Errorf("AssetDigest %s not found", name)
}

// Digests returns a map of all known files and their checksums.
func Digests() (map[string][sha256.Size]byte, error) {
	mp := make(map[string][sha256.Size]byte, len(_bindata))
	for name := range _bindata {
		a, err := _bindata[name]()
		if err != nil {
			return nil, err
		}
		mp[name] = a.digest
	}
	return mp, nil
}

// AssetNames returns the names of the assets.
func AssetNames() []string {
	names := make([]string, 0, len(_bindata))
	for name := range _bindata {
		names = append(names, name)
	}
	return names
}

// _bindata is a table, holding each asset generator, mapped to its name.
var _bindata = map[string]func() (*asset, error){
	"0001_create_notebooks_table.sql": _0001_create_notebooks_tableSql,
}

// AssetDir returns the file names below a certain
// directory embedded in the file by go-bindata.
// For example if you run go-bindata on data/... and data contains the
// following hierarchy:
//     data/
//       foo.txt
//       img/
//         a.png
//         b.png
// then AssetDir("data") would return []string{"foo.txt", "img"},
// AssetDir("data/img") would return []string{"a.png", "b.png"},
// AssetDir("foo.txt") and AssetDir("notexist") would return an error, and
// AssetDir("") will return []string{"data"}.
func AssetDir(name string) ([]string, error) {
	node := _bintree
	if len(name) != 0 {
		canonicalName := strings.Replace(name, "\\", "/", -1)
		pathList := strings.Split(canonicalName, "/")
		for _, p := range pathList {
			node = node.Children[p]
			if node == nil {
				return nil, fmt.Errorf("Asset %s not found", name)
			}
		}
	}
	if node.Func != nil {
		return nil, fmt.Errorf("Asset %s not found", name)
	}
	rv := make([]string, 0, len(node.Children))
	for childName := range node.Children {
		rv = append(rv, childName)
	}
	return rv, nil
}

type bintree struct {
	Func     func() (*asset, error)
	Children map[string]*bintree
}

var _bintree = &bintree{nil, map[string]*bintree{
	"0001_create_notebooks_table.sql": &bintree{_0001_create_notebooks_tableSql, map[string]*bintree{}},
}}

// RestoreAsset restores an asset under the given directory.
func RestoreAsset(dir, name string) error {
	data, err := Asset(name)
	if err != nil {
		return err
	}
	info, err := AssetInfo(name)
	if err != nil {
		return err
	}
	err = os.MkdirAll(_filePath(dir, filepath.Dir(name)), os.FileMode(0755))
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(_filePath(dir, name), data, info.Mode())
	if err != nil {
		return err
	}
	return os.Chtimes(_filePath(dir, name), info.ModTime(), info.ModTime())
}

// RestoreAssets restores an asset under the given directory recursively.
func RestoreAssets(dir, name string) error {
	children, err := AssetDir(name)
	// File
	if err != nil {
		return RestoreAsset(dir, name)
	}
	// Dir
	for _, child := range children {
		err = RestoreAssets(dir, filepath.Join(name, child))
		if err != nil {
			return err
		}
	}
	return nil
}

func _filePath(dir, name string) string {
	canonicalName := strings.Replace(name, "\\", "/", -1)
	return filepath.Join(append([]string{dir}, strings.Split(canonicalName, "/")...)...)
}
