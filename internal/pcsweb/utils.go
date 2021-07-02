package pcsweb

import (
	"github.com/Erope/BaiduPCS-Go/internal/pcscommand"
	"github.com/Erope/BaiduPCS-Go/pcsutil/converter"
	"html/template"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type FileDesc struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"isdir"`
	Size  string `json:"size"`
	MDate string `json:"date"`
}

type FileDescs []FileDesc

func (fds FileDescs) Len() int {
	return len(fds)
}

func (fds FileDescs) Less(i, j int) bool {
	if fds[i].IsDir == fds[j].IsDir {
		return fds[i].Name < fds[j].Name
	}

	if fds[i].IsDir {
		return true
	}
	return false
}

func (fds FileDescs) Swap(i, j int) {
	temp := fds[i]
	fds[i] = fds[j]
	fds[j] = temp
}


func matchPathByShellPatternOnce(pattern *string) error {
	paths, err := pcscommand.GetBaiduPCS().MatchPathByShellPattern(pcscommand.GetActiveUser().PathJoin(*pattern))
	if err != nil {
		return err
	}
	switch len(paths) {
	case 0:
		return pcscommand.ErrShellPatternNoHit
	case 1:
		*pattern = paths[0]
	default:
		return pcscommand.ErrShellPatternMultiRes
	}

	return nil
}

func matchPathByShellPattern(patterns ...string) (pcspaths []string, err error) {
	acUser, pcs := pcscommand.GetActiveUser(), pcscommand.GetBaiduPCS()
	for k := range patterns {
		ps, err := pcs.MatchPathByShellPattern(acUser.PathJoin(patterns[k]))
		if err != nil {
			return nil, err
		}

		pcspaths = append(pcspaths, ps...)
	}
	return pcspaths, nil
}

// boxTmplParse ricebox 载入文件内容, 并进行模板解析
func boxTmplParse(name string, fileNames ...string) (tmpl *template.Template) {
	tmpl = template.New(name)
	for k := range fileNames {
		tmpl.Parse(distBox.MustString(fileNames[k]))
	}
	return
}

//获取指定目录下的所有文件，不进入下一级目录搜索，可以匹配后缀过滤。
func ListLocalDir(dirPth string, suffix string) (files FileDescs, err error) {
	files = make(FileDescs, 0, 10)

	if dirPth == "." {
		dirPth, _ = filepath.Abs(filepath.Dir(os.Args[0]))
	}

	dirPth = strings.Replace(dirPth, "\\", "/", -1)


	dir, err := ioutil.ReadDir(dirPth)
	if err != nil {
		return nil, err
	}

	var suffixFlag = false
	if suffix == "" {
		suffixFlag = true
		suffix = strings.ToUpper(suffix) //忽略后缀匹配的大小写
	}

	for _, fi := range dir {
		if suffixFlag && !strings.HasSuffix(strings.ToUpper(fi.Name()), suffix) { //匹配文件
			continue
		}
		files = append(files, FileDesc{
			Name:  fi.Name(),
			Path:  dirPth + "/" + fi.Name(),
			IsDir: fi.IsDir(),
			Size:  converter.ConvertFileSize(fi.Size(), 2),
			MDate: fi.ModTime().String(),
		})
	}
	sort.Sort(files)
	return files, nil
}
