package git

import (
	"compress/zlib"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
)

var InvalidObject error = errors.New("Invalid object")

type GitObject interface {
	GetType() string
	GetContent() []byte
	GetSize() int
}

type GitBlobObject struct {
	size    int
	content []byte
}

func (GitBlobObject) GetType() string {
	return "blob"
}
func (b GitBlobObject) GetContent() []byte {
	if len(b.content) != b.size {
		panic(fmt.Sprintf("Content of blob does not match size. %d != %d", len(b.content), b.size))
	}
	return b.content
}
func (b GitBlobObject) GetSize() int {
	return b.size
}
func (c *Client) GetObject(sha1 Sha1) (GitObject, error) {
	_, packed, err := c.HaveObject(sha1.String())
	if packed == true {
		return nil, fmt.Errorf("GetObject does not yet support packed objects")
	}
	if err != nil {
		panic(err)
	}
	objectname := fmt.Sprintf("%s/objects/%x/%x", c.GitDir, sha1[0:1], sha1[1:])
	fmt.Printf("File: %s\n", objectname)
	f, err := os.Open(objectname)
	if err != nil {
		panic("Couldn't open object file.")
	}
	defer f.Close()

	uncompressed, err := zlib.NewReader(f)
	if err != nil {
		return nil, err
	}
	b, err := ioutil.ReadAll(uncompressed)
	if err != nil {
		return nil, err
	}
	if string(b[0:5]) == "blob " {
		var size int
		var content []byte
		for idx, val := range b {
			if val == 0 {
				content = b[idx+1:]
				if size, err = strconv.Atoi(string(b[5:idx])); err != nil {
					fmt.Printf("Error converting % x to int at idx: %d", b[5:idx], idx)
				}
				break
			}
		}
		return GitBlobObject{size, content}, nil
	} else {
		fmt.Printf("Content: %s\n", string(b))
	}
	return nil, InvalidObject
}