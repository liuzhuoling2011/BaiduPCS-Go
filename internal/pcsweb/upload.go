package pcsweb

import (
	"bytes"
	"container/list"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/Erope/BaiduPCS-Go/baidupcs"
	"github.com/Erope/BaiduPCS-Go/baidupcs/pcserror"
	"github.com/Erope/BaiduPCS-Go/internal/pcscommand"
	"github.com/Erope/BaiduPCS-Go/internal/pcsconfig"
	"github.com/Erope/BaiduPCS-Go/internal/pcsfunctions/pcsupload"
	"github.com/Erope/BaiduPCS-Go/pcsutil"
	"github.com/Erope/BaiduPCS-Go/pcsutil/checksum"
	"github.com/Erope/BaiduPCS-Go/pcsutil/converter"
	"github.com/Erope/BaiduPCS-Go/requester/rio"
	"github.com/Erope/BaiduPCS-Go/requester/uploader"
	"golang.org/x/net/websocket"
)

const (
	// DefaultUploadMaxRetry 默认上传失败最大重试次数
	DefaultUploadMaxRetry = 3
)

type (
	// UploadOptions 上传可选项
	UploadOptions struct {
		Parallel       int
		MaxRetry       int
		NotRapidUpload bool
		NotSplitFile   bool // 禁用分片上传
	}

	// StepUpload 上传步骤
	StepUpload int

	utask struct {
		ListTask
		localFileChecksum *checksum.LocalFileChecksum // 要上传的本地文件详情
		step              StepUpload
		savePath          string
	}
)

const (
	// StepUploadInit 初始化步骤
	StepUploadInit StepUpload = iota
	// StepUploadRapidUpload 秒传步骤
	StepUploadRapidUpload
	// StepUploadUpload 正常上传步骤
	StepUploadUpload
)

// RunRapidUpload 执行秒传文件, 前提是知道文件的大小, md5, 前256KB切片的 md5, crc32
func RunRapidUpload(targetPath, contentMD5, sliceMD5, crc32 string, length int64) {
	err := matchPathByShellPatternOnce(&targetPath)
	if err != nil {
		fmt.Printf("警告: %s, 获取网盘路径 %s 错误, %s\n", baidupcs.OperationRapidUpload, targetPath, err)
	}

	err = pcscommand.GetBaiduPCS().RapidUpload(targetPath, contentMD5, sliceMD5, crc32, length)
	if err != nil {
		fmt.Printf("%s失败, 消息: %s\n", baidupcs.OperationRapidUpload, err)
		return
	}

	fmt.Printf("%s成功, 保存到网盘路径: %s\n", baidupcs.OperationRapidUpload, targetPath)
	return
}

// RunCreateSuperFile 执行分片上传—合并分片文件
func RunCreateSuperFile(targetPath string, blockList ...string) {
	err := matchPathByShellPatternOnce(&targetPath)
	if err != nil {
		fmt.Printf("警告: %s, 获取网盘路径 %s 错误, %s\n", baidupcs.OperationUploadCreateSuperFile, targetPath, err)
	}

	err = pcscommand.GetBaiduPCS().UploadCreateSuperFile(targetPath, blockList...)
	if err != nil {
		fmt.Printf("%s失败, 消息: %s\n", baidupcs.OperationUploadCreateSuperFile, err)
		return
	}

	fmt.Printf("%s成功, 保存到网盘路径: %s\n", baidupcs.OperationUploadCreateSuperFile, targetPath)
	return
}

// RunUpload 执行文件上传
func RunUpload(conn *websocket.Conn, localPaths []string, savePath string, opt *UploadOptions) {
	if opt == nil {
		opt = &UploadOptions{}
	}

	// 检测opt
	if opt.Parallel <= 0 {
		opt.Parallel = pcsconfig.Config.MaxUploadParallel
	}

	if opt.MaxRetry < 0 {
		opt.MaxRetry = DefaultUploadMaxRetry
	}

	err := matchPathByShellPatternOnce(&savePath)
	if err != nil {
		fmt.Printf("警告: 上传文件, 获取网盘路径 %s 错误, %s\n", savePath, err)
	}

	switch len(localPaths) {
	case 0:
		fmt.Printf("本地路径为空\n")
		return
	}

	var (
		pcs           = pcscommand.GetBaiduPCS()
		ulist         = list.New()
		lastID        int
		globedPathDir string
		subSavePath   string
	)

	for k := range localPaths {
		walkedFiles, err := pcsutil.WalkDir(localPaths[k], "")
		if err != nil {
			fmt.Printf("警告: %s\n", err)
			continue
		}

		for k3 := range walkedFiles {
			// 针对 windows 的目录处理
			if os.PathSeparator == '\\' {
				walkedFiles[k3] = pcsutil.ConvertToUnixPathSeparator(walkedFiles[k3])
				globedPathDir = pcsutil.ConvertToUnixPathSeparator(filepath.Dir(localPaths[k]))
			} else {
				globedPathDir = filepath.Dir(localPaths[k])
			}

			// 避免去除文件名开头的"."
			if globedPathDir == "." {
				globedPathDir = ""
			}

			subSavePath = strings.TrimPrefix(walkedFiles[k3], globedPathDir)

			lastID++
			ulist.PushBack(&utask{
				ListTask: ListTask{
					ID:       lastID,
					MaxRetry: opt.MaxRetry,
				},
				localFileChecksum: checksum.NewLocalFileChecksum(walkedFiles[k3], int(baidupcs.SliceMD5Size)),
				savePath:          path.Clean(savePath + baidupcs.PathSeparator + subSavePath),
			})

			fmt.Printf("[%d] 加入上传队列: %s\n", lastID, walkedFiles[k3])
			MsgBody = fmt.Sprintf("{\"LastID\": %d, \"path\": \"%s\"}", lastID, walkedFiles[k3])
			sendResponse(conn, 3, 1, "添加进任务队列", MsgBody, true, true)
		}
	}

	if lastID == 0 {
		fmt.Printf("未检测到上传的文件, 请检查文件路径或通配符是否正确.\n")
		return
	}

	uploadDatabase, err := pcsupload.NewUploadingDatabase()
	if err != nil {
		sendResponse(conn, 3, -1, "打开上传未完成数据库错误", "", true, true)
		fmt.Printf("打开上传未完成数据库错误: %s\n", err)
		return
	}
	defer uploadDatabase.Close()

	var (
		handleTaskErr = func(task *utask, errManifest string, pcsError pcserror.Error) {
			if task == nil {
				panic("task is nil")
			}

			if pcsError == nil {
				return
			}

			// 不重试的情况
			switch pcsError.GetErrType() {
			// 远程服务器错误
			case pcserror.ErrTypeRemoteError:
				switch pcsError.GetRemoteErrCode() {
				case 31200: //[Method:Insert][Error:Insert Request Forbid]
				// do nothing, continue
				default:
					fmt.Printf("[%d] %s, %s\n", task.ID, errManifest, pcsError)
					return
				}
			case pcserror.ErrTypeNetError:
				if strings.Contains(pcsError.GetError().Error(), "413 Request Entity Too Large") {
					fmt.Printf("[%d] %s, %s\n", task.ID, errManifest, pcsError)
					return
				}
			}

			// 未达到失败重试最大次数, 将任务推送到队列末尾
			if task.retry < task.MaxRetry {
				task.retry++
				MsgBody = fmt.Sprintf("{\"LastID\": %d, \"errManifest\": \"%s\", \"error\": \"%s\", \"retry\": %d, \"max_retry\": %d}", task.ID, errManifest, pcsError, task.retry, task.MaxRetry)
				sendResponse(conn, 3, -2, "重试", MsgBody, true, true)
				fmt.Printf("[%d] %s, %s, 重试 %d/%d\n", task.ID, errManifest, pcsError, task.retry, task.MaxRetry)
				ulist.PushBack(task)
				time.Sleep(3 * time.Duration(task.retry) * time.Second)
			} else {
				// on failed
				fmt.Printf("[%d] %s, %s\n", task.ID, errManifest, pcsError)
				sendResponse(conn, 3, -3, "上传任务失败", "", true, true)
			}
		}
		totalSize int64
	)

	for {
		e := ulist.Front()
		if e == nil { // 结束
			break
		}

		ulist.Remove(e) // 载入任务后, 移除队列

		task := e.Value.(*utask)

		func() {
			fmt.Printf("[%d] 准备上传: %s\n", task.ID, task.localFileChecksum.Path)
			MsgBody = fmt.Sprintf("{\"LastID\": %d, \"path\": \"%s\"}", task.ID, task.localFileChecksum.Path)
			sendResponse(conn, 3, 2, "准备上传", MsgBody, true, true)

			err = task.localFileChecksum.OpenPath()
			if err != nil {
				fmt.Printf("[%d] 文件不可读, 错误信息: %s, 跳过...\n", task.ID, err)
				MsgBody = fmt.Sprintf("{\"LastID\": %d, \"error\": \"%s\"}", task.ID, err)
				sendResponse(conn, 3, -4, "文件不可读, 跳过", MsgBody, true, true)
				return
			}
			defer task.localFileChecksum.Close() // 关闭文件

			var (
				panDir, panFile = path.Split(task.savePath)
			)
			panDir = path.Clean(panDir)

			// 检测断点续传
			state := uploadDatabase.Search(&task.localFileChecksum.LocalFileMeta)
			if state != nil || task.localFileChecksum.LocalFileMeta.MD5 != nil { // 读取到了md5
				task.step = StepUploadUpload
				goto stepControl
			}

			if opt.NotRapidUpload {
				task.step = StepUploadUpload
				goto stepControl
			}

			if task.localFileChecksum.Length > baidupcs.MaxRapidUploadSize {
				fmt.Printf("[%d] 文件超过20GB, 无法使用秒传功能, 跳过秒传...\n", task.ID)
				task.step = StepUploadUpload
				goto stepControl
			}

		stepControl: // 步骤控制
			switch task.step {
			case StepUploadRapidUpload:
				goto stepUploadRapidUpload
			case StepUploadUpload:
				goto stepUploadUpload
			}

		stepUploadRapidUpload:
			// 文件大于256kb, 应该要检测秒传, 反之则不应检测秒传
			// 经测试, 秒传文件并非一定要大于256KB
			task.step = StepUploadRapidUpload
			{
				fdl, pcsError := pcs.CacheFilesDirectoriesList(panDir, baidupcs.DefaultOrderOptions)
				if pcsError != nil {
					switch pcsError.GetErrType() {
					case pcserror.ErrTypeRemoteError:
						// do nothing
					default:
						fmt.Printf("获取文件列表错误, %s\n", pcsError)
						return
					}
				}

				if task.localFileChecksum.Length >= 128*converter.MB {
					fmt.Printf("[%d] 检测秒传中, 请稍候...\n", task.ID)
				}

				// 经测试, 文件的 crc32 值并非秒传文件所必需
				task.localFileChecksum.Sum(checksum.CHECKSUM_MD5 | checksum.CHECKSUM_SLICE_MD5)

				// 检测缓存, 通过文件的md5值判断本地文件和网盘文件是否一样
				if fdl != nil {
					for _, fd := range fdl {
						if fd.Filename == panFile {
							decodedMD5, _ := hex.DecodeString(fd.MD5)
							if bytes.Compare(decodedMD5, task.localFileChecksum.MD5) == 0 {
								fmt.Printf("[%d] 目标文件, %s, 已存在, 跳过...\n", task.ID, task.savePath)
								MsgBody = fmt.Sprintf("{\"LastID\": %d, \"savePath\": \"%s\"}", task.ID, task.savePath)
								sendResponse(conn, 3, 3, "目标文件已存在, 跳过", MsgBody, true, true)
								return
							}
						}
					}
				}

				pcsError = pcs.RapidUpload(task.savePath, hex.EncodeToString(task.localFileChecksum.MD5), hex.EncodeToString(task.localFileChecksum.SliceMD5), fmt.Sprint(task.localFileChecksum.CRC32), task.localFileChecksum.Length)
				if pcsError == nil {
					fmt.Printf("[%d] 秒传成功, 保存到网盘路径: %s\n\n", task.ID, task.savePath)
					MsgBody = fmt.Sprintf("{\"LastID\": %d, \"savePath\": \"%s\"}", task.ID, task.savePath)
					sendResponse(conn, 3, 3, "秒传成功", MsgBody, true, true)
					totalSize += task.localFileChecksum.Length
					return
				}

				// 判断配额是否已满
				switch pcsError.GetErrType() {
				// 远程服务器错误
				case pcserror.ErrTypeRemoteError:
					switch pcsError.GetRemoteErrCode() {
					case 31112: //exceed quota
						fmt.Printf("[%d] 秒传失败, 超出配额, 网盘容量已满\n\n", task.ID)
						return
					}
				}
			}

			fmt.Printf("[%d] 秒传失败, 开始上传文件...\n\n", task.ID)

			// 保存秒传信息
			uploadDatabase.UpdateUploading(&task.localFileChecksum.LocalFileMeta, nil)
			uploadDatabase.Save()

			// 秒传失败, 开始上传文件
		stepUploadUpload:
			task.step = StepUploadUpload
			{
				var blockSize int64
				if opt.NotSplitFile {
					blockSize = task.localFileChecksum.Length
				} else {
					blockSize = getBlockSize(task.localFileChecksum.Length)
				}

				muer := uploader.NewMultiUploader(pcsupload.NewPCSUpload(pcs, task.savePath), rio.NewFileReaderAtLen64(task.localFileChecksum.GetFile()), &uploader.MultiUploaderConfig{
					Parallel:  opt.Parallel,
					BlockSize: blockSize,
					MaxRate:   pcsconfig.Config.MaxUploadRate,
				})

				// 设置断点续传
				if state != nil {
					muer.SetInstanceState(state)
				}

				exitChan := make(chan struct{})
				muer.OnUploadStatusEvent(func(status uploader.Status, updateChan <-chan struct{}) {
					select {
					case <-updateChan:
						uploadDatabase.UpdateUploading(&task.localFileChecksum.LocalFileMeta, muer.InstanceState())
						uploadDatabase.Save()
					default:
					}

					var leftStr string

					uploaded, totalSize, speeds := status.Uploaded(), status.TotalSize(), status.SpeedsPerSecond()
					if speeds <= 0 {
						leftStr = "-"
					} else {
						leftStr = (time.Duration((totalSize-uploaded)/(speeds)) * time.Second).String()
					}

					var avgSpeed int64 = 0
					timeUsed := status.TimeElapsed() / 1e7 * 1e7
					timeSecond := status.TimeElapsed().Seconds()
					if int64(timeSecond) > 0 {
						avgSpeed = uploaded / int64(timeSecond)
					}

					fmt.Printf("\r[%d] ↑ %s/%s %s/s in %s ............", task.ID,
						converter.ConvertFileSize(uploaded, 2),
						converter.ConvertFileSize(totalSize, 2),
						converter.ConvertFileSize(speeds, 2),
						status.TimeElapsed(),
					)
					MsgBody = fmt.Sprintf("{\"LastID\": %d, \"uploaded_size\": \"%s\", \"total_size\": \"%s\", \"percent\": %.2f, \"speed\": \"%s\", \"avg_speed\": \"%s\", \"time_used\": \"%s\", \"time_left\": \"%s\"}", task.ID,
						converter.ConvertFileSize(uploaded, 2),
						converter.ConvertFileSize(totalSize, 2),
						float64(uploaded)/float64(totalSize)*100,
						converter.ConvertFileSize(speeds, 2),
						converter.ConvertFileSize(avgSpeed, 2),
						timeUsed, leftStr)
					sendResponse(conn, 3, 4, "上传中", MsgBody, true, true)
				})
				muer.OnSuccess(func() {
					close(exitChan)
					MsgBody = fmt.Sprintf("{\"LastID\": %d, \"savePath\": \"%s\"}", task.ID, task.savePath)
					sendResponse(conn, 3, 5, "上传文件成功", MsgBody, true, true)

					fmt.Printf("\n")
					fmt.Printf("[%d] 上传文件成功, 保存到网盘路径: %s\n", task.ID, task.savePath)
					totalSize += task.localFileChecksum.Length
					uploadDatabase.Delete(&task.localFileChecksum.LocalFileMeta) // 删除
					uploadDatabase.Save()
				})
				muer.OnError(func(err error) {
					close(exitChan)
					pcsError, ok := err.(pcserror.Error)
					if !ok {
						fmt.Printf("[%d] 上传文件错误: %s\n", task.ID, err)
						return
					}

					switch pcsError.GetRemoteErrCode() {
					case 31363: // block miss in superfile2, 上传状态过期
						uploadDatabase.Delete(&task.localFileChecksum.LocalFileMeta)
						uploadDatabase.Save()
						fmt.Printf("[%d] 上传文件错误: 上传状态过期, 请重新上传\n", task.ID)
						return
					}

					handleTaskErr(task, "上传文件失败", pcsError)
					return
				})
				muer.Execute()
			}
		}()
	}

	fmt.Printf("\n")
	fmt.Printf("全部上传完毕, 总大小: %s\n", converter.ConvertFileSize(totalSize))
}

func getBlockSize(fileSize int64) int64 {
	blockNum := fileSize / baidupcs.MinUploadBlockSize
	if blockNum > 999 {
		return fileSize/999 + 1
	}
	return baidupcs.MinUploadBlockSize
}
