package pcsweb

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/Erope/BaiduPCS-Go/baidupcs"
	"github.com/Erope/BaiduPCS-Go/internal/pcscommand"
	"github.com/Erope/BaiduPCS-Go/internal/pcsconfig"
	"github.com/Erope/BaiduPCS-Go/pcsutil/converter"
	"github.com/Erope/BaiduPCS-Go/pcsverbose"
)

var (
	pcsCommandVerbose = pcsverbose.New("PCSCOMMAND")
	Version           = "3.6.8"
)

//func PasswordHandle(w http.ResponseWriter, r *http.Request) {
//	r.ParseForm()
//	method := r.Form.Get("method")
//	switch method {
//	case "lock":
//		password := pcsconfig.Config.AccessPass()
//		if password != "" {
//			GlobalSessions.Lock(w, r)
//			sendHttpResponse(w, "success", "")
//			return
//		}
//		sendHttpErrorResponse(w, -6, "请先设置锁定密码")
//	case "exist":
//		password := pcsconfig.Config.AccessPass()
//		if password != "" {
//			sendHttpResponse(w, "", true)
//			return
//		}
//		sendHttpResponse(w, "", false)
//	case "verify":
//		password := pcsconfig.Config.AccessPass()
//		pass := r.Form.Get("password")
//		if pass == password {
//			sendHttpResponse(w, "", true)
//			return
//		}
//		sendHttpResponse(w, "", false)
//	case "set":
//		password := pcsconfig.Config.AccessPass()
//		oldpass := r.Form.Get("oldpass")
//		if password != "" && oldpass != password {
//			sendHttpErrorResponse(w, -3, "密码输入错误")
//			return
//		}
//
//		pass := r.Form.Get("password")
//		pcsconfig.Config.SetAccessPass(pass)
//		if err := pcsconfig.Config.Save(); err != nil {
//			sendHttpErrorResponse(w, -2, "保存配置错误: "+err.Error())
//			return
//		}
//		sendHttpResponse(w, "", "")
//	}
//}

func LoginHandle(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Access-Control-Allow-Headers", "Content-Type") //header的类型

	r.ParseForm()
	bduss := r.Form.Get("bduss")
	if bduss == "" { // CheckLock
		if GlobalSessions.CheckLock(w, r) {
			fmt.Println("当前未登录，需要进行登录")
			sendHttpErrorResponse(w, -3, "需要解锁")
		} else {
			fmt.Println("已经登录，无需再次登录")
			sendHttpResponse(w, "无需解锁", "")
		}
		return
	}

	b, err := pcsconfig.Config.SetupUserByBDUSS(bduss, "", "")
	if err != nil {
		fmt.Println("BDUSS登录失败...")
		sendHttpErrorResponse(w, -2, "BDUSS登录失败: "+err.Error())
		return
	}

	pcsconfig.Config.SwitchUser(&pcsconfig.BaiduBase{
		Name: b.Name,
	})

	GlobalSessions.UnLock(w, r)

	if err = pcsconfig.Config.Save(); err != nil {
		sendHttpErrorResponse(w, -2, "保存配置错误: "+err.Error())
		return
	}

	sendHttpResponse(w, "账户登录成功", b)
}

func bdHandle(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	path = strings.TrimPrefix(path, "/bd/")
	path = strings.Replace(path, "http:/", "http://", 1)
	path = strings.Replace(path, "https:/", "https://", 1)
	remote, err := url.Parse(path)
	if err != nil {
		panic(err)
	}
	r.URL.Host = remote.Host
	r.URL.Path = remote.Path
	r.URL.Scheme = remote.Scheme
	r.Host = remote.Host
	remote.Path = ""
	if r.Header.Get("range") != "" || r.Header.Get("Range") != "" {
		http.Redirect(w, r, path+"?"+r.URL.RawQuery, http.StatusMovedPermanently)
		return
	} else {
		r.Method = "GET"
		r.Header.Add("Range", "bytes=0-0")
	}
	if strings.HasSuffix(remote.Host, ".baidupcs.com") {
		proxy := httputil.NewSingleHostReverseProxy(remote)
		proxy.ServeHTTP(w, r)
	}
}

func UserHandle(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	method := r.Form.Get("method")
	switch method {
	case "list":
		sendHttpResponse(w, "", pcsconfig.Config.BaiduUserList)
	case "get":
		activeUser := pcsconfig.Config.ActiveUser()
		sendHttpResponse(w, "", activeUser)
	case "set":
		name := r.Form.Get("name")
		_, err := pcsconfig.Config.SwitchUser(&pcsconfig.BaiduBase{
			Name: name,
		})
		if err != nil {
			var uid uint64
			for _, user := range pcsconfig.Config.BaiduUserList {
				if user.Name == name {
					uid = user.UID
				}
			}
			_, err = pcsconfig.Config.SwitchUser(&pcsconfig.BaiduBase{
				UID: uid,
			})
			if err != nil {
				sendHttpErrorResponse(w, -1, "切换用户失败: "+err.Error())
				return
			}
		}

		if err = pcsconfig.Config.Save(); err != nil {
			sendHttpErrorResponse(w, -2, "保存配置错误: "+err.Error())
			return
		}

		activeUser := pcsconfig.Config.ActiveUser()
		sendHttpResponse(w, "", activeUser)
	}
}

func QuotaHandle(w http.ResponseWriter, r *http.Request) {
	quota, used, _ := pcsconfig.Config.ActiveUserBaiduPCS().QuotaInfo()
	quotaMsg := fmt.Sprintf("{\"quota\": \"%s\", \"used\": \"%s\", \"un_used\": \"%s\", \"percent\": %.2f}",
		converter.ConvertFileSize(quota, 2),
		converter.ConvertFileSize(used, 2),
		converter.ConvertFileSize(quota-used, 2),
		100*float64(used)/float64(quota))
	pcsCommandVerbose.Info(quotaMsg)
	sendHttpResponse(w, "", quotaMsg)
}

func DownloadHandle(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	method := r.Form.Get("method")
	id, _ := strconv.Atoi(r.Form.Get("id"))
	pcsCommandVerbose.Info("下载管理:" + method + ", " + r.Form.Get("id"))

	dl := DownloaderMap[id]
	if dl == nil {
		sendHttpErrorResponse(w, -6, "任务已经终结")
		return
	}

	response := &Response{
		Code: 0,
		Msg:  "success",
	}
	switch method {
	case "pause":
		dl.Pause()
	case "resume":
		dl.Resume()
	case "cancel":
		dl.Cancel()
	case "status":
		response.Data = dl.GetAllWorkersStatus()
	}
	w.Write(response.JSON())
}

func OfflineDownloadHandle(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	method := r.Form.Get("method")
	pcsCommandVerbose.Info("离线下载:" + method)

	switch method {
	case "list":
		cl, err := pcsconfig.Config.ActiveUserBaiduPCS().CloudDlListTask()
		if err != nil {
			sendHttpErrorResponse(w, -1, err.Error())
			return
		}
		sendHttpResponse(w, "", cl)
	case "delete":
		id, _ := strconv.Atoi(r.Form.Get("id"))
		err := pcsconfig.Config.ActiveUserBaiduPCS().CloudDlDeleteTask(int64(id))
		if err != nil {
			sendHttpErrorResponse(w, -1, err.Error())
			return
		}
		sendHttpResponse(w, "", "")
	case "cancel":
		id, _ := strconv.Atoi(r.Form.Get("id"))
		err := pcsconfig.Config.ActiveUserBaiduPCS().CloudDlCancelTask(int64(id))
		if err != nil {
			sendHttpErrorResponse(w, -1, err.Error())
			return
		}
		sendHttpResponse(w, "", "")
	case "add":
		link := r.Form.Get("link")
		tpath := r.Form.Get("tpath")
		taskid, err := pcsconfig.Config.ActiveUserBaiduPCS().CloudDlAddTask(link, tpath)
		if err != nil {
			sendHttpErrorResponse(w, -1, err.Error())
			return
		}
		sendHttpResponse(w, strconv.Itoa(int(taskid)), "")
	}
}

func SearchHandle(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	tpath := r.Form.Get("tpath")
	keyword := r.Form.Get("keyword")
	pcsCommandVerbose.Info("搜索:" + tpath + " " + keyword)

	files, err := pcsconfig.Config.ActiveUserBaiduPCS().Search(tpath, keyword, true)
	if err != nil {
		sendHttpErrorResponse(w, -1, err.Error())
		return
	}
	sendHttpResponse(w, "", files)
}

func RecycleHandle(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	rmethod := r.Form.Get("method")
	pcsCommandVerbose.Info(rmethod)

	if rmethod == "list" {
		recycle, err := pcsconfig.Config.ActiveUserBaiduPCS().RecycleList(1)
		if err != nil {
			sendHttpErrorResponse(w, -1, err.Error())
			return
		}
		sendHttpResponse(w, "", recycle)
	}
	if rmethod == "clear" {
		_, err := pcsconfig.Config.ActiveUserBaiduPCS().RecycleClear()
		if err != nil {
			sendHttpErrorResponse(w, -1, err.Error())
			return
		}
		sendHttpResponse(w, "", "")
	}
	if rmethod == "restore" {
		rfid := r.Form.Get("fid")
		fid := converter.MustInt64(rfid)
		_, err := pcsconfig.Config.ActiveUserBaiduPCS().RecycleRestore(fid)
		if err != nil {
			sendHttpErrorResponse(w, -1, err.Error())
			return
		}
		sendHttpResponse(w, "", "")
	}
	if rmethod == "delete" {
		rfid := r.Form.Get("fid")
		fid := converter.MustInt64(rfid)
		err := pcsconfig.Config.ActiveUserBaiduPCS().RecycleDelete(fid)
		if err != nil {
			sendHttpErrorResponse(w, -1, err.Error())
			return
		}
		sendHttpResponse(w, "", "")
	}
}

func ShareHandle(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	rmethod := r.Form.Get("method")
	pcsCommandVerbose.Info(rmethod)

	if rmethod == "list" {
		records, err := pcsconfig.Config.ActiveUserBaiduPCS().ShareList(1)
		if err != nil {
			sendHttpErrorResponse(w, -1, err.Error())
			return
		}
		sendHttpResponse(w, "", records)
	}
	if rmethod == "cancel" {
		rids := strings.Split(r.Form.Get("id"), ",")
		ids := make([]int64, 0, 10)
		for _, sid := range rids {
			tmp, _ := strconv.Atoi(sid)
			ids = append(ids, int64(tmp))
		}
		err := pcsconfig.Config.ActiveUserBaiduPCS().ShareCancel(ids)
		if err != nil {
			sendHttpErrorResponse(w, -1, err.Error())
			return
		}
		sendHttpResponse(w, "success", "")
	}
	if rmethod == "set" {
		rpath := r.Form.Get("paths")
		rpaths := strings.Split(rpath, "|")
		paths := make([]string, 0, 10)
		for _, path := range rpaths {
			paths = append(paths, path)
		}
		fmt.Println(rpath, paths)
		shared, err := pcsconfig.Config.ActiveUserBaiduPCS().ShareSet(paths, nil)
		if err != nil {
			sendHttpErrorResponse(w, -1, err.Error())
			return
		}
		sendHttpResponse(w, shared.Link, "")
	}
}

func OptionsHandle(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	rmethod := r.Form.Get("method")
	rtype := r.Form.Get("type")
	pcsCommandVerbose.Info("配置: " + rmethod)

	configJsons := make([]pcsOptsJSON, 0, 10)
	config := pcsconfig.Config
	if rmethod == "get" {
		if rtype == "download" {
			opts := config.DownloadOpts()
			configJsons = append(configJsons, pcsOptsJSON{
				Name:  "no_check",
				Value: opts.NoCheck,
				Desc:  "下载文件完成后不校验文件",
			})
			configJsons = append(configJsons, pcsOptsJSON{
				Name:  "overwrite",
				Value: opts.IsOverwrite,
				Desc:  "下载时覆盖已存在的文件",
			})
			configJsons = append(configJsons, pcsOptsJSON{
				Name:  "add_x",
				Value: opts.IsExecutedPermission,
				Desc:  "为文件加上执行权限, (windows系统无效)",
			})
			configJsons = append(configJsons, pcsOptsJSON{
				Name:  "stream",
				Value: opts.IsStreaming,
				Desc:  "以流式文件的方式下载",
			})
			configJsons = append(configJsons, pcsOptsJSON{
				Name:  "share",
				Value: opts.IsShareDownload,
				Desc:  "以分享文件的方式获取下载链接来下载",
			})
			configJsons = append(configJsons, pcsOptsJSON{
				Name:  "locate",
				Value: opts.IsLocateDownload,
				Desc:  "以获取直链的方式来下载",
			})
			configJsons = append(configJsons, pcsOptsJSON{
				Name:  "locate_pan",
				Value: opts.IsLocatePanAPIDownload,
				Desc:  "从百度网盘首页获取直链来下载, 该下载方式需配合第三方服务器, 机密文件切勿使用此下载方式",
			})
			sendHttpResponse(w, "", configJsons)
		}
	}
	if rmethod == "set" {
		if rtype == "download" {
			no_check, _ := strconv.ParseBool(r.Form.Get("no_check"))
			overwrite, _ := strconv.ParseBool(r.Form.Get("overwrite"))
			add_x, _ := strconv.ParseBool(r.Form.Get("add_x"))
			stream, _ := strconv.ParseBool(r.Form.Get("stream"))
			share, _ := strconv.ParseBool(r.Form.Get("share"))
			locate, _ := strconv.ParseBool(r.Form.Get("locate"))
			locate_pan, _ := strconv.ParseBool(r.Form.Get("locate_pan"))
			opt := pcsconfig.CDownloadOptions{
				NoCheck:                no_check,
				IsOverwrite:            overwrite,
				IsExecutedPermission:   add_x,
				IsStreaming:            stream,
				IsShareDownload:        share,
				IsLocateDownload:       locate,
				IsLocatePanAPIDownload: locate_pan,
			}
			config.SetDownloadOpts(opt)
			config.Save()
			sendHttpResponse(w, "success", configJsons)
		}
	}
}

func SettingHandle(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	rmethod := r.Form.Get("method")
	pcsCommandVerbose.Info("设置: " + rmethod)

	config := pcsconfig.Config
	if rmethod == "get" {
		configJsons := make([]pcsConfigJSON, 0, 10)
		configJsons = append(configJsons, pcsConfigJSON{
			Name:   "PCS应用ID",
			EnName: "appid",
			Value:  strconv.Itoa(config.AppID),
			Desc:   "",
		})
		configJsons = append(configJsons, pcsConfigJSON{
			Name:   "启用 https",
			EnName: "enable_https",
			Value:  fmt.Sprint(config.EnableHTTPS),
			Desc:   "",
		})
		configJsons = append(configJsons, pcsConfigJSON{
			Name:   "浏览器标识",
			EnName: "user_agent",
			Value:  config.UserAgent,
			Desc:   "",
		})
		configJsons = append(configJsons, pcsConfigJSON{
			Name:   "下载缓存",
			EnName: "cache_size",
			Value:  converter.ConvertFileSize(int64(config.CacheSize), 2),
			Desc:   "建议1KB ~ 256KB, 单位不区分大小写(如64KB, 1MB, 32kb, 65536b, 65536), 如果硬盘占用高或下载速度慢, 请尝试调大此值",
		})
		configJsons = append(configJsons, pcsConfigJSON{
			Name:   "下载最大并发量",
			EnName: "max_parallel",
			Value:  strconv.Itoa(config.MaxParallel),
			Desc:   "建议50 ~ 500. 单任务下载最大线程数量",
		})
		configJsons = append(configJsons, pcsConfigJSON{
			Name:   "上传最大并发量",
			EnName: "max_upload_parallel",
			Value:  strconv.Itoa(config.MaxUploadParallel),
			Desc:   "建议1 ~ 100. 单任务上传最大线程数量",
		})
		configJsons = append(configJsons, pcsConfigJSON{
			Name:   "同时下载数量",
			EnName: "max_download_load",
			Value:  strconv.Itoa(config.MaxDownloadLoad),
			Desc:   "建议 1 ~ 5, 同时进行下载文件的最大数量",
		})
		configJsons = append(configJsons, pcsConfigJSON{
			Name:   "限制最大下载速度",
			EnName: "max_download_rate",
			Value:  converter.ConvertFileSize(int64(config.MaxDownloadRate), 2) + "/s",
			Desc:   "0代表不限制, 单位为每秒的传输速率(如2MB/s, 2MB, 2m, 2mb, 2097152b, 2097152, 后缀'/s' 可省略)",
		})
		configJsons = append(configJsons, pcsConfigJSON{
			Name:   "限制最大上传速度",
			EnName: "max_upload_rate",
			Value:  converter.ConvertFileSize(int64(config.MaxUploadRate), 2) + "/s",
			Desc:   "0代表不限制, 单位为每秒的传输速率(如 2MB/s, 2MB, 2m, 2mb, 2097152b, 2097152, 后缀'/s' 可省略)",
		})		
		configJsons = append(configJsons, pcsConfigJSON{
			Name:   "下载目录",
			EnName: "savedir",
			Value:  config.SaveDir,
			Desc:   "下载文件的储存目录",
		})
		configJsons = append(configJsons, pcsConfigJSON{
			Name:   "工作目录",
			EnName: "workdir",
			Value:  pcsconfig.Config.ActiveUser().Workdir,
			Desc:   "程序启动时打开的目录，例如 /apps/baidu_shurufa",
		})
		envVar, ok := os.LookupEnv(pcsconfig.EnvConfigDir)
		if !ok {
			envVar = pcsconfig.GetConfigDir()
		}
		configJsons = append(configJsons, pcsConfigJSON{
			Name:   "配置文件目录",
			EnName: "config_dir",
			Value:  envVar,
			Desc:   "配置文件的储存目录，更改无效",
		})
		sendHttpResponse(w, "", configJsons)
	}
	if rmethod == "set" {
		config := pcsconfig.Config

		appid := r.Form.Get("appid")
		int_value, _ := strconv.Atoi(appid)
		if int_value != config.AppID {
			config.SetAppID(int_value)
		}

		enable_https := r.Form.Get("enable_https")
		bool_value, _ := strconv.ParseBool(enable_https)
		if bool_value != config.EnableHTTPS {
			config.SetEnableHTTPS(bool_value)
		}

		user_agent := r.Form.Get("user_agent")
		if user_agent != config.UserAgent {
			config.SetUserAgent(user_agent)
		}

		cache_size := r.Form.Get("cache_size")
		byte_size, err := converter.ParseFileSizeStr(cache_size)
		if err != nil {
			sendHttpErrorResponse(w, -1, "设置 cache_size 错误")
			config.Save()
			return
		}
		int_value = int(byte_size)
		if int_value != config.CacheSize {
			config.CacheSize = int_value
		}

		max_parallel := r.Form.Get("max_parallel")
		int_value, _ = strconv.Atoi(max_parallel)
		if int_value != config.MaxParallel {
			config.MaxParallel = int_value
		}

		max_download_load := r.Form.Get("max_download_load")
		int_value, _ = strconv.Atoi(max_download_load)
		if int_value != config.MaxDownloadLoad {
			config.MaxDownloadLoad = int_value
		}

		max_upload_parallel := r.Form.Get("max_upload_parallel")
		int_value, _ = strconv.Atoi(max_upload_parallel)
		if int_value != config.MaxUploadParallel {
			config.MaxUploadParallel = int_value
		}

		max_download_rate := r.Form.Get("max_download_rate")
		err = pcsconfig.Config.SetMaxDownloadRateByStr(max_download_rate)
		if err != nil {
			sendHttpErrorResponse(w, -1, "设置 max_download_rate 错误")
			config.Save()
			return
		}
		
		max_upload_rate := r.Form.Get("max_upload_rate")
		err = pcsconfig.Config.SetMaxUploadRateByStr(max_upload_rate)
		if err != nil {
			sendHttpErrorResponse(w, -1, "设置 max_upload_rate 错误")
			config.Save()
			return
		}

		savedir := r.Form.Get("savedir")
		_, err = ioutil.ReadDir(savedir)
		if err != nil {
			sendHttpErrorResponse(w, -1, "输入的本地文件夹路径错误，请检查目录是否存在或者具有可写权限")
			config.Save()
			return
		}
		config.SaveDir = savedir

		workdir := r.Form.Get("workdir")
		err = pcscommand.RunChangeDirectory(workdir, false)
		if err != nil {
			sendHttpErrorResponse(w, -1, "设置的百度云目录不存在或者不可读，请检查appid是否有权限")
			config.Save()
			return
		}

		config.Save()
	}
	if rmethod == "update" {
		/* url := "http://www.zoranjojo.top:9925/api/v1/update?goos=" + runtime.GOOS + "&goarch=" + runtime.GOARCH + "&version=" + Version
		resp, err := http.Get(url)
		if err != nil {
			sendHttpErrorResponse(w, -1, "查找版本更新失败")
		}
		defer resp.Body.Close()
		body, err := ioutil.ReadAll(resp.Body)

		if err != nil {
			sendHttpErrorResponse(w, -2, "查找版本更新失败")
		}
		sendHttpResponse(w, "", string(body)) */
		sendHttpErrorResponse(w, -1, "关闭在线更新通道")
	}
	if rmethod == "notice" {
		/* url := "http://www.zoranjojo.top:9925/api/v1/notice"
		resp, err := http.Get(url)
		if err != nil {
			sendHttpErrorResponse(w, -1, "查找通知信息失败")
		}
		defer resp.Body.Close()
		body, err := ioutil.ReadAll(resp.Body)

		if err != nil {
			sendHttpErrorResponse(w, -2, "查找通知信息失败")
		}

		sendHttpResponse(w, "", string(body)) */
		sendHttpErrorResponse(w, -1, "关闭在线通知")
	}
}

func LogoutHandle(w http.ResponseWriter, r *http.Request) {
	activeUser := pcsconfig.Config.ActiveUser()
	deletedUser, err := pcsconfig.Config.DeleteUser(&pcsconfig.BaiduBase{
		UID: activeUser.UID,
	})
	if err != nil {
		fmt.Printf("退出用户 %s, 失败, 错误: %s\n", activeUser.Name, err)
	}

	fmt.Printf("退出用户成功, %s\n", deletedUser.Name)
	err = pcsconfig.Config.Save()
	if err != nil {
		fmt.Printf("保存配置错误: %s\n", err)
	}
	fmt.Printf("保存配置成功\n")
	GlobalSessions.SessionDestroy(w, r)
}

func LocalFileHandle(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	rmethod := r.Form.Get("method")
	rpath := r.Form.Get("path")
	pcsCommandVerbose.Info("本地文件操作:" + rmethod + " " + rpath)

	if rmethod == "list" {
		files, err := ListLocalDir(rpath, "")
		if err != nil {
			sendHttpErrorResponse(w, -1, err.Error())
			return
		}
		sendHttpResponse(w, "", files)
		return
	}
	if rmethod == "open_folder" {
		tmp := strings.Split(rpath, "/")
		if runtime.GOOS == "windows" {
			path := strings.Join(tmp[:len(tmp)-1], "\\")
			cmd := exec.Command("explorer", path)
			cmd.Run()
			sendHttpResponse(w, "", "")
		} else if runtime.GOOS == "linux" {
			path := strings.Join(tmp[:len(tmp)-1], "/")
			cmd := exec.Command("nautilus", path)
			cmd.Run()
			sendHttpResponse(w, "", "")
		} else if runtime.GOOS == "darwin" {
			path := strings.Join(tmp[:len(tmp)-1], "/")
			cmd := exec.Command("open", path)
			cmd.Run()
			sendHttpResponse(w, "", "")
		} else {
			sendHttpErrorResponse(w, -1, "不支持的系统")
		}
		return
	}
}

func FileOperationHandle(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	rmethod := r.Form.Get("method")
	rpaths := r.Form.Get("paths")
	pcsCommandVerbose.Info("远程文件操作:" + rmethod + " " + rpaths)

	paths := strings.Split(rpaths, "|")
	var err error
	if rmethod == "copy" {
		err = pcscommand.RunCopy(paths...)
	} else if rmethod == "move" {
		err = pcscommand.RunMove(paths...)
	} else if rmethod == "remove" {
		err = pcscommand.RunRemove(paths...)
	} else {
		sendHttpErrorResponse(w, -2, "方法调用错误")
	}
	if err != nil {
		sendHttpErrorResponse(w, -2, err.Error())
		return
	}
	sendHttpResponse(w, "success", "")
}

func MkdirHandle(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	rpath := r.Form.Get("path")
	pcsCommandVerbose.Info("远程新建文件夹:" + rpath)

	err := pcscommand.RunMkdir(rpath)
	if err != nil {
		sendHttpErrorResponse(w, -1, err.Error())
		return
	}
	sendHttpResponse(w, "success", "")
}

func fileList(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()

	fpath := r.Form.Get("path")
	orderBy := r.Form.Get("order_by")
	order := r.Form.Get("order")
	pcsCommandVerbose.Info("获取目录:" + fpath + " " + orderBy + " " + order)

	orderOptions := &baidupcs.OrderOptions{}
	switch {
	case order == "asc":
		orderOptions.Order = baidupcs.OrderAsc
	case order == "desc":
		orderOptions.Order = baidupcs.OrderDesc
	default:
		orderOptions.Order = baidupcs.OrderAsc
	}

	switch {
	case orderBy == "time":
		orderOptions.By = baidupcs.OrderByTime
	case orderBy == "name":
		orderOptions.By = baidupcs.OrderByName
	case orderBy == "size":
		orderOptions.By = baidupcs.OrderBySize
	default:
		orderOptions.By = baidupcs.OrderByName
	}

	dataReadCloser, err := pcsconfig.Config.ActiveUserBaiduPCS().PrepareFilesDirectoriesList(fpath, orderOptions)

	w.Header().Set("content-type", "application/json")

	if err != nil {
		sendHttpErrorResponse(w, -1, err.Error())
		return
	}

	defer dataReadCloser.Close()
	io.Copy(w, dataReadCloser)
}
