package main

import (
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/Erope/BaiduPCS-Go/baidupcs"
	"github.com/Erope/BaiduPCS-Go/internal/pcscommand"
	"github.com/Erope/BaiduPCS-Go/internal/pcsconfig"
	_ "github.com/Erope/BaiduPCS-Go/internal/pcsinit"
	"github.com/Erope/BaiduPCS-Go/internal/pcsweb"
	"github.com/Erope/BaiduPCS-Go/pcstable"
	"github.com/Erope/BaiduPCS-Go/pcsutil"
	"github.com/Erope/BaiduPCS-Go/pcsutil/checksum"
	"github.com/Erope/BaiduPCS-Go/pcsutil/converter"
	"github.com/Erope/BaiduPCS-Go/pcsverbose"
	"github.com/olekukonko/tablewriter"
	"github.com/urfave/cli"
)

const (
	// NameShortDisplayNum 文件名缩略显示长度
	NameShortDisplayNum = 16

	cryptoDescription = `
	可用的方法 <method>:
		aes-128-ctr, aes-192-ctr, aes-256-ctr,
		aes-128-cfb, aes-192-cfb, aes-256-cfb,
		aes-128-ofb, aes-192-ofb, aes-256-ofb.

	密钥 <key>:
		aes-128 对应key长度为16, aes-192 对应key长度为24, aes-256 对应key长度为32,
		如果key长度不符合, 则自动修剪key, 舍弃超出长度的部分, 长度不足的部分用'\0'填充.

	GZIP <disable-gzip>:
		在文件加密之前, 启用GZIP压缩文件; 文件解密之后启用GZIP解压缩文件, 默认启用,
		如果不启用, 则无法检测文件是否解密成功, 解密文件时会保留源文件, 避免解密失败造成文件数据丢失.`
)

var (
	// Version 版本号
	Version = "v3.7.3"

	historyFilePath = filepath.Join(pcsconfig.GetConfigDir(), "pcs_command_history.txt")
	reloadFn        = func(c *cli.Context) error {
		err := pcsconfig.Config.Reload()
		if err != nil {
			fmt.Printf("重载配置错误: %s\n", err)
		}
		return nil
	}
	saveFunc = func(c *cli.Context) error {
		err := pcsconfig.Config.Save()
		if err != nil {
			fmt.Printf("保存配置错误: %s\n", err)
		}
		return nil
	}
	isCli bool
)

func init() {
	pcsutil.ChWorkDir()

	err := pcsconfig.Config.Init()
	switch err {
	case nil:
	case pcsconfig.ErrConfigFileNoPermission, pcsconfig.ErrConfigContentsParseError:
		fmt.Fprintf(os.Stderr, "FATAL ERROR: config file error: %s\n", err)
		os.Exit(1)
	default:
		fmt.Printf("WARNING: config init error: %s\n", err)
	}

	if pcsweb.GlobalSessions == nil {
		pcsweb.GlobalSessions, err = pcsweb.NewSessionManager("memory", "goSessionid", 90*24*3600)
		if err != nil {
			fmt.Println(err)
			return
		}
		pcsweb.GlobalSessions.Init()
		go pcsweb.GlobalSessions.GC()
	}
}

func main() {
	defer pcsconfig.Config.Close()
	app := cli.NewApp()
	app.Name = "BaiduPCS-Go"
	app.Version = Version
	zponds := cli.Author{
		Name:  "zponds",
		Email: "wjhjd163@gmail.com",
	}
	liuzhuoling := cli.Author{
		Name:  "liuzhuoling",
		Email: "liuzhuoling2011@hotmail.com",
	}
	iikira := cli.Author{
		Name:  "iikira",
		Email: "i@mail.iikira.com",
	}
	app.Authors = []cli.Author{liuzhuoling, iikira, zponds}
	app.Description = "BaiduPCS-Go 使用Go语言编写的百度网盘命令行客户端, 可以让你高效的使用百度云"
	app.Flags = []cli.Flag{
		cli.BoolFlag{
			Name:        "verbose",
			Usage:       "启用调试",
			EnvVar:      pcsverbose.EnvVerbose,
			Destination: &pcsverbose.IsVerbose,
		},
		cli.BoolFlag{
			Name:        "aria2, a",
			Usage:       "启用aria2下载，停用自带下载",
			Destination: &pcsweb.Aria2,
		},
		cli.StringFlag{
			Name:        "aria2url, au",
			Usage:       "aria2的url",
			Value:       "http://localhost:6800/jsonrpc",
			Destination: &pcsweb.Aria2_Url,
		},
		cli.StringFlag{
			Name:        "aria2secret, as",
			Usage:       "aria2-RPC的secret，默认为空",
			Value:       "",
			Destination: &pcsweb.Aria2_Secret,
		},
		cli.StringFlag{
			Name:        "aria2pre, ap",
			Usage:       "aria2-RPC添加下载链接时附加在前面的前缀，用于解决可能出现下载时的403问题和拉黑后无法下载问题，默认为空，注意，前缀是以Aria2服务器为起始点的!例子: http://localhost:5299/bd/",
			Value:       "",
			Destination: &pcsweb.Aria2_prefix,
		},
		cli.StringFlag{
			Name:        "pdurl, pd",
			Usage:       "使用 https://github.com/TkzcM/baiduwp 搭建的Pandownload搭建网站加速下载的网址，如 https://pandl.live/ ，注意需要输入开头的https或http和末尾的/，默认不使用",
			Value:       "",
			Destination: &pcsweb.PD_Url,
		},
	}
	app.Action = func(c *cli.Context) {
		if pcsweb.Aria2 {
			fmt.Printf("已经启用Aria2下载，停用默认下载，下载列表会为空，仍在开发中，可能不稳定\n")
		}
		fmt.Printf("打开浏览器, 输入 http://localhost:5299 查看效果\n")
		//对于Windows和Mac，调用系统默认浏览器打开 http://localhost:5299
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = exec.Command("CMD", "/C", "start", "http://localhost:5299")
			if err := cmd.Start(); err != nil {
				fmt.Println(err.Error())
			}
		} else if runtime.GOOS == "darwin" {
			cmd = exec.Command("open", "http://localhost:5299")
			if err := cmd.Start(); err != nil {
				fmt.Println(err.Error())
			}
		}

		if err := pcsweb.StartServer(5299, true); err != nil {
			fmt.Println(err.Error())
		}
	}
	app.Commands = []cli.Command{
		{
			Name:     "web",
			Usage:    "启用 web 客户端",
			Category: "其他",
			Before:   reloadFn,
			Action: func(c *cli.Context) error {
				fmt.Printf("打开浏览器, 输入: http://localhost:%d 查看效果\n", c.Uint("port"))
				fmt.Println(pcsweb.StartServer(c.Uint("port"), c.Bool("access")))
				return nil
			},
			Flags: []cli.Flag{
				cli.UintFlag{
					Name:  "port",
					Usage: "自定义端口",
					Value: 5299,
				},
				cli.BoolFlag{
					Name:   "access",
					Usage:  "是否允许外网访问",
					Hidden: false,
				},
			},
		},
		{
			Name:     "env",
			Usage:    "显示程序环境变量",
			Category: "其他",
			Description: `
				BAIDUPCS_GO_CONFIG_DIR: 配置文件路径,
				BAIDUPCS_GO_VERBOSE: 是否启用调试.
			`,
			Action: func(c *cli.Context) error {
				envStr := "%s=\"%s\"\n"
				envVar, ok := os.LookupEnv(pcsverbose.EnvVerbose)
				if ok {
					fmt.Printf(envStr, pcsverbose.EnvVerbose, envVar)
				} else {
					fmt.Printf(envStr, pcsverbose.EnvVerbose, "0")
				}

				envVar, ok = os.LookupEnv(pcsconfig.EnvConfigDir)
				if ok {
					fmt.Printf(envStr, pcsconfig.EnvConfigDir, envVar)
				} else {
					fmt.Printf(envStr, pcsconfig.EnvConfigDir, pcsconfig.GetConfigDir())
				}

				return nil
			},
		},
		{
			Name:  "login",
			Usage: "登录百度账号",
			Description: `
				示例:
					BaiduPCS-Go login
					BaiduPCS-Go login -username=liuhua
					BaiduPCS-Go login -bduss=123456789
				常规登录:
					按提示一步一步来即可.
				百度BDUSS获取方法:
					参考这篇 Wiki: https://github.com/Erope/BaiduPCS-Go/wiki/关于-获取百度-BDUSS
					或者百度搜索: 获取百度BDUSS
			`,
			Category: "百度帐号",
			Before:   reloadFn,
			After:    saveFunc,
			Action: func(c *cli.Context) error {
				var bduss, ptoken, stoken string
				if c.IsSet("bduss") {
					bduss = c.String("bduss")
					ptoken = c.String("ptoken")
					stoken = c.String("stoken")
				} else if c.NArg() == 0 {
					var err error
					bduss, ptoken, stoken, err = pcscommand.RunLogin(c.String("username"), c.String("password"))
					if err != nil {
						fmt.Println(err)
						return err
					}
				} else {
					cli.ShowCommandHelp(c, c.Command.Name)
					return nil
				}

				baidu, err := pcsconfig.Config.SetupUserByBDUSS(bduss, ptoken, stoken)
				if err != nil {
					fmt.Println(err)
					return nil
				}

				fmt.Println("百度帐号登录成功:", baidu.Name)
				return nil
			},
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "username",
					Usage: "登录百度帐号的用户名(手机号/邮箱/用户名)",
				},
				cli.StringFlag{
					Name:  "password",
					Usage: "登录百度帐号的用户名的密码",
				},
				cli.StringFlag{
					Name:  "bduss",
					Usage: "使用百度 BDUSS 来登录百度帐号",
				},
				cli.StringFlag{
					Name:  "ptoken",
					Usage: "百度 PTOKEN, 配合 -bduss 参数使用 (可选)",
				},
				cli.StringFlag{
					Name:  "stoken",
					Usage: "百度 STOKEN, 配合 -bduss 参数使用 (可选)",
				},
			},
		},
		{
			Name:  "su",
			Usage: "切换百度帐号",
			Description: `
				切换已登录的百度帐号:
				如果运行该条命令没有提供参数, 程序将会列出所有的百度帐号, 供选择切换.
				示例:
				BaiduPCS-Go su
				BaiduPCS-Go su <uid or name>
			`,
			Category: "百度帐号",
			Before:   reloadFn,
			After:    saveFunc,
			Action: func(c *cli.Context) error {
				if c.NArg() >= 2 {
					cli.ShowCommandHelp(c, c.Command.Name)
					return nil
				}

				numLogins := pcsconfig.Config.NumLogins()

				if numLogins == 0 {
					fmt.Printf("未设置任何百度帐号, 不能切换\n")
					return nil
				}

				var (
					inputData = c.Args().Get(0)
					uid       uint64
				)

				if c.NArg() == 1 {
					// 直接切换
					uid, _ = strconv.ParseUint(inputData, 10, 64)
				} else if c.NArg() == 0 {
					// 输出所有帐号供选择切换
					cli.HandleAction(app.Command("loglist").Action, c)

					// 提示输入 index
					var index string
					fmt.Printf("输入要切换帐号的 # 值 > ")
					_, err := fmt.Scanln(&index)
					if err != nil {
						return nil
					}

					if n, err := strconv.Atoi(index); err == nil && n >= 0 && n < numLogins {
						uid = pcsconfig.Config.BaiduUserList[n].UID
					} else {
						fmt.Printf("切换用户失败, 请检查 # 值是否正确\n")
						return nil
					}
				} else {
					cli.ShowCommandHelp(c, c.Command.Name)
				}

				switchedUser, err := pcsconfig.Config.SwitchUser(&pcsconfig.BaiduBase{
					Name: inputData,
				})
				if err != nil {
					switchedUser, err = pcsconfig.Config.SwitchUser(&pcsconfig.BaiduBase{
						UID: uid,
					})
					if err != nil {
						fmt.Printf("切换用户失败, %s\n", err)
						return nil
					}
				}

				fmt.Printf("切换用户: %s\n", switchedUser.Name)
				return nil
			},
		},
		{
			Name:        "logout",
			Usage:       "退出百度帐号",
			Description: "退出当前登录的百度帐号",
			Category:    "百度帐号",
			Before:      reloadFn,
			After:       saveFunc,
			Action: func(c *cli.Context) error {
				if pcsconfig.Config.NumLogins() == 0 {
					fmt.Println("未设置任何百度帐号, 不能退出")
					return nil
				}

				var (
					confirm    string
					activeUser = pcsconfig.Config.ActiveUser()
				)

				if !c.Bool("y") {
					fmt.Printf("确认退出百度帐号: %s ? (y/n) > ", activeUser.Name)
					_, err := fmt.Scanln(&confirm)
					if err != nil || (confirm != "y" && confirm != "Y") {
						return err
					}
				}

				deletedUser, err := pcsconfig.Config.DeleteUser(&pcsconfig.BaiduBase{
					UID: activeUser.UID,
				})
				if err != nil {
					fmt.Printf("退出用户 %s, 失败, 错误: %s\n", activeUser.Name, err)
				}

				fmt.Printf("退出用户成功, %s\n", deletedUser.Name)
				return nil
			},
			Flags: []cli.Flag{
				cli.BoolFlag{
					Name:  "y",
					Usage: "确认退出帐号",
				},
			},
		},

		{
			Name:        "loglist",
			Usage:       "列出帐号列表",
			Description: "列出所有已登录的百度帐号",
			Category:    "百度帐号",
			Before:      reloadFn,
			Action: func(c *cli.Context) error {
				fmt.Println(pcsconfig.Config.BaiduUserList.String())
				return nil
			},
		},
		{
			Name:        "who",
			Usage:       "获取当前帐号",
			Description: "获取当前帐号的信息",
			Category:    "百度帐号",
			Before:      reloadFn,
			Action: func(c *cli.Context) error {
				activeUser := pcsconfig.Config.ActiveUser()
				fmt.Printf("当前帐号 uid: %d, 用户名: %s, 性别: %s, 年龄: %.1f\n", activeUser.UID, activeUser.Name, activeUser.Sex, activeUser.Age)
				return nil
			},
		},
		{
			Name:        "quota",
			Usage:       "获取网盘配额",
			Description: "获取网盘的总储存空间, 和已使用的储存空间",
			Category:    "百度网盘",
			Before:      reloadFn,
			Action: func(c *cli.Context) error {
				pcscommand.RunGetQuota()
				return nil
			},
		},
		{
			Name:     "cd",
			Category: "百度网盘",
			Usage:    "切换工作目录",
			Description: `
				BaiduPCS-Go cd <目录, 绝对路径或相对路径>
				示例:
				切换 /我的资源 工作目录:
				BaiduPCS-Go cd /我的资源
				切换上级目录:
				BaiduPCS-Go cd ..
				切换根目录:
				BaiduPCS-Go cd /
				切换 /我的资源 工作目录, 并自动列出 /我的资源 下的文件和目录
				BaiduPCS-Go cd -l 我的资源
				使用通配符:
				BaiduPCS-Go cd /我的*
			`,
			Before: reloadFn,
			After:  saveFunc,
			Action: func(c *cli.Context) error {
				if c.NArg() == 0 {
					cli.ShowCommandHelp(c, c.Command.Name)
					return nil
				}

				pcscommand.RunChangeDirectory(c.Args().Get(0), c.Bool("l"))

				return nil
			},
			Flags: []cli.Flag{
				cli.BoolFlag{
					Name:  "l",
					Usage: "切换工作目录后自动列出工作目录下的文件和目录",
				},
			},
		},
		{
			Name:      "ls",
			Aliases:   []string{"l", "ll"},
			Usage:     "列出目录",
			UsageText: app.Name + " ls <目录>",
			Description: `
				列出当前工作目录内的文件和目录, 或指定目录内的文件和目录
				示例:
				列出 我的资源 内的文件和目录
				BaiduPCS-Go ls 我的资源
				绝对路径
				BaiduPCS-Go ls /我的资源
				降序排序
				BaiduPCS-Go ls -desc 我的资源
				按文件大小降序排序
				BaiduPCS-Go ls -size -desc 我的资源
				使用通配符
				BaiduPCS-Go ls /我的*
			`,
			Category: "百度网盘",
			Before:   reloadFn,
			Action: func(c *cli.Context) error {
				orderOptions := &baidupcs.OrderOptions{}
				switch {
				case c.IsSet("asc"):
					orderOptions.Order = baidupcs.OrderAsc
				case c.IsSet("desc"):
					orderOptions.Order = baidupcs.OrderDesc
				default:
					orderOptions.Order = baidupcs.OrderAsc
				}

				switch {
				case c.IsSet("time"):
					orderOptions.By = baidupcs.OrderByTime
				case c.IsSet("name"):
					orderOptions.By = baidupcs.OrderByName
				case c.IsSet("size"):
					orderOptions.By = baidupcs.OrderBySize
				default:
					orderOptions.By = baidupcs.OrderByName
				}

				pcscommand.RunLs(c.Args().Get(0), &pcscommand.LsOptions{
					Total: c.Bool("l") || c.Parent().Args().Get(0) == "ll",
				}, orderOptions)

				return nil
			},
			Flags: []cli.Flag{
				cli.BoolFlag{
					Name:  "l",
					Usage: "详细显示",
				},
				cli.BoolFlag{
					Name:  "asc",
					Usage: "升序排序",
				},
				cli.BoolFlag{
					Name:  "desc",
					Usage: "降序排序",
				},
				cli.BoolFlag{
					Name:  "time",
					Usage: "根据时间排序",
				},
				cli.BoolFlag{
					Name:  "name",
					Usage: "根据文件名排序",
				},
				cli.BoolFlag{
					Name:  "size",
					Usage: "根据大小排序",
				},
			},
		},
		{
			Name:      "search",
			Aliases:   []string{"s"},
			Usage:     "搜索文件",
			UsageText: app.Name + " search [-path=<需要检索的目录>] [-r] 关键字",
			Description: `
				按文件名搜索文件（不支持查找目录）。
				默认在当前工作目录搜索.
				示例:
				搜索根目录的文件
				BaiduPCS-Go search -path=/ 关键字
				搜索当前工作目录的文件
				BaiduPCS-Go search 关键字
				递归搜索当前工作目录的文件
				BaiduPCS-Go search -r 关键字
			`,
			Category: "百度网盘",
			Before:   reloadFn,
			Action: func(c *cli.Context) error {
				if c.NArg() < 1 {
					cli.ShowCommandHelp(c, c.Command.Name)
					return nil
				}

				pcscommand.RunSearch(c.String("path"), c.Args().Get(0), &pcscommand.SearchOptions{
					Total:   c.Bool("l"),
					Recurse: c.Bool("r"),
				})

				return nil
			},
			Flags: []cli.Flag{
				cli.BoolFlag{
					Name:  "l",
					Usage: "详细显示",
				},
				cli.BoolFlag{
					Name:  "r",
					Usage: "递归搜索",
				},
				cli.StringFlag{
					Name:  "path",
					Usage: "需要检索的目录",
					Value: ".",
				},
			},
		},
		{
			Name:      "tree",
			Aliases:   []string{"t"},
			Usage:     "列出目录的树形图",
			UsageText: app.Name + " tree <目录>",
			Category:  "百度网盘",
			Before:    reloadFn,
			Action: func(c *cli.Context) error {
				pcscommand.RunTree(c.Args().Get(0))
				return nil
			},
		},
		{
			Name:      "pwd",
			Usage:     "输出工作目录",
			UsageText: app.Name + " pwd",
			Category:  "百度网盘",
			Before:    reloadFn,
			Action: func(c *cli.Context) error {
				fmt.Println(pcsconfig.Config.ActiveUser().Workdir)
				return nil
			},
		},
		{
			Name:        "meta",
			Usage:       "获取文件/目录的元信息",
			UsageText:   app.Name + " meta <文件/目录1> <文件/目录2> <文件/目录3> ...",
			Description: "默认获取工作目录元信息",
			Category:    "百度网盘",
			Before:      reloadFn,
			Action: func(c *cli.Context) error {
				var (
					ca = c.Args()
					as []string
				)
				if len(ca) == 0 {
					as = []string{""}
				} else {
					as = ca
				}

				pcscommand.RunGetMeta(as...)
				return nil
			},
		},
		{
			Name:      "rm",
			Usage:     "删除文件/目录",
			UsageText: app.Name + " rm <文件/目录的路径1> <文件/目录2> <文件/目录3> ...",
			Description: `
				注意: 删除多个文件和目录时, 请确保每一个文件和目录都存在, 否则删除操作会失败.
				被删除的文件或目录可在网盘文件回收站找回.
				示例:
				删除 /我的资源/1.mp4
				BaiduPCS-Go rm /我的资源/1.mp4
				删除 /我的资源/1.mp4 和 /我的资源/2.mp4
				BaiduPCS-Go rm /我的资源/1.mp4 /我的资源/2.mp4
				删除 /我的资源 内的所有文件和目录, 但不删除该目录
				BaiduPCS-Go rm /我的资源/*
				删除 /我的资源 整个目录 !!
				BaiduPCS-Go rm /我的资源
			`,
			Category: "百度网盘",
			Before:   reloadFn,
			Action: func(c *cli.Context) error {
				if c.NArg() == 0 {
					cli.ShowCommandHelp(c, c.Command.Name)
					return nil
				}

				pcscommand.RunRemove(c.Args()...)
				return nil
			},
		},
		{
			Name:      "mkdir",
			Usage:     "创建目录",
			UsageText: app.Name + " mkdir <目录>",
			Category:  "百度网盘",
			Before:    reloadFn,
			Action: func(c *cli.Context) error {
				if c.NArg() == 0 {
					cli.ShowCommandHelp(c, c.Command.Name)
					return nil
				}

				pcscommand.RunMkdir(c.Args().Get(0))
				return nil
			},
		},
		{
			Name:  "cp",
			Usage: "拷贝文件/目录",
			UsageText: `BaiduPCS-Go cp <文件/目录> <目标文件/目录>
				BaiduPCS-Go cp <文件/目录1> <文件/目录2> <文件/目录3> ... <目标目录>`,
			Description: `
				注意: 拷贝多个文件和目录时, 请确保每一个文件和目录都存在, 否则拷贝操作会失败.
				示例:
				将 /我的资源/1.mp4 复制到 根目录 /
				BaiduPCS-Go cp /我的资源/1.mp4 /
				将 /我的资源/1.mp4 和 /我的资源/2.mp4 复制到 根目录 /
				BaiduPCS-Go cp /我的资源/1.mp4 /我的资源/2.mp4 /
			`,
			Category: "百度网盘",
			Before:   reloadFn,
			Action: func(c *cli.Context) error {
				if c.NArg() <= 1 {
					cli.ShowCommandHelp(c, c.Command.Name)
					return nil
				}

				pcscommand.RunCopy(c.Args()...)
				return nil
			},
		},
		{
			Name:  "mv",
			Usage: "移动/重命名文件/目录",
			UsageText: `移动:
				BaiduPCS-Go mv <文件/目录1> <文件/目录2> <文件/目录3> ... <目标目录>
				重命名:
				BaiduPCS-Go mv <文件/目录> <重命名的文件/目录>`,
			Description: `
				注意: 移动多个文件和目录时, 请确保每一个文件和目录都存在, 否则移动操作会失败.
				示例:
				将 /我的资源/1.mp4 移动到 根目录 /
				BaiduPCS-Go mv /我的资源/1.mp4 /
				将 /我的资源/1.mp4 重命名为 /我的资源/3.mp4
				BaiduPCS-Go mv /我的资源/1.mp4 /我的资源/3.mp4
			`,
			Category: "百度网盘",
			Before:   reloadFn,
			Action: func(c *cli.Context) error {
				if c.NArg() <= 1 {
					cli.ShowCommandHelp(c, c.Command.Name)
					return nil
				}

				pcscommand.RunMove(c.Args()...)
				return nil
			},
		},
		{
			Name:      "download",
			Aliases:   []string{"d"},
			Usage:     "下载文件/目录",
			UsageText: app.Name + " download <文件/目录路径1> <文件/目录2> <文件/目录3> ...",
			Description: `
				下载的文件默认保存到, 程序所在目录的 download/ 目录.
				通过 BaiduPCS-Go config set -savedir <savedir>, 自定义保存的目录.
				已支持目录下载.
				已支持多个文件或目录下载.
				已支持下载完成后自动校验文件, 但并不是所有的文件都支持校验!
				自动跳过下载重名的文件!
				示例:
				设置保存目录, 保存到 D:\Downloads
				注意区别反斜杠 "\" 和 斜杠 "/" !!!
				BaiduPCS-Go config set -savedir D:\\Downloads
				或者
				BaiduPCS-Go config set -savedir D:/Downloads
				下载 /我的资源/1.mp4
				BaiduPCS-Go d /我的资源/1.mp4
				下载 /我的资源 整个目录!!
				BaiduPCS-Go d /我的资源
				下载网盘内的全部文件!!
				BaiduPCS-Go d /
				BaiduPCS-Go d *
			`,
			Category: "百度网盘",
			Before:   reloadFn,
			Action: func(c *cli.Context) error {
				if c.NArg() == 0 {
					cli.ShowCommandHelp(c, c.Command.Name)
					return nil
				}

				var (
					saveTo string
				)

				if c.Bool("save") {
					saveTo = "."
				} else if c.String("saveto") != "" {
					saveTo = filepath.Clean(c.String("saveto"))
				}

				do := &pcscommand.DownloadOptions{
					IsTest:                 c.Bool("test"),
					IsPrintStatus:          c.Bool("status"),
					IsExecutedPermission:   c.Bool("x") && runtime.GOOS != "windows",
					IsOverwrite:            c.Bool("ow"),
					IsShareDownload:        c.Bool("share"),
					IsLocateDownload:       c.Bool("locate"),
					IsLocatePanAPIDownload: c.Bool("locate_pan"),
					IsStreaming:            c.Bool("stream"),
					SaveTo:                 saveTo,
					Parallel:               c.Int("p"),
					Load:                   c.Int("l"),
					MaxRetry:               c.Int("retry"),
					NoCheck:                c.Bool("nocheck"),
				}

				if c.Bool("bg") && isCli {
					pcscommand.RunBgDownload(c.Args(), do)
				} else {
					pcscommand.RunDownload(c.Args(), do)
				}

				return nil
			},
			Flags: []cli.Flag{
				cli.BoolFlag{
					Name:  "test",
					Usage: "测试下载, 此操作不会保存文件到本地",
				},
				cli.BoolFlag{
					Name:  "ow",
					Usage: "overwrite, 覆盖已存在的文件",
				},
				cli.BoolFlag{
					Name:  "status",
					Usage: "输出所有线程的工作状态",
				},
				cli.BoolFlag{
					Name:  "save",
					Usage: "将下载的文件直接保存到当前工作目录",
				},
				cli.StringFlag{
					Name:  "saveto",
					Usage: "将下载的文件直接保存到指定的目录",
				},
				cli.BoolFlag{
					Name:  "x",
					Usage: "为文件加上执行权限, (windows系统无效)",
				},
				cli.BoolFlag{
					Name:  "stream",
					Usage: "以流式文件的方式下载",
				},
				cli.BoolFlag{
					Name:  "share",
					Usage: "以分享文件的方式获取下载链接来下载",
				},
				cli.BoolFlag{
					Name:  "locate",
					Usage: "以获取直链的方式来下载",
				},
				cli.BoolFlag{
					Name:  "locate_pan",
					Usage: "从百度网盘首页获取直链来下载, 该下载方式需配合第三方服务器, 机密文件切勿使用此下载方式",
				},
				cli.IntFlag{
					Name:  "p",
					Usage: "指定下载线程数",
				},
				cli.IntFlag{
					Name:  "l",
					Usage: "指定同时进行下载文件的数量",
				},
				cli.IntFlag{
					Name:  "retry",
					Usage: "下载失败最大重试次数",
					Value: pcscommand.DefaultDownloadMaxRetry,
				},
				cli.BoolFlag{
					Name:  "nocheck",
					Usage: "下载文件完成后不校验文件",
				},
				cli.BoolFlag{
					Name:  "bg",
					Usage: "加入后台下载",
				},
			},
		},
		{
			Name:  "bg",
			Usage: "管理后台任务",
			Description: `
				默认关闭下载中任何向终端的输出
				再后台进行文件下载，不会影响用户继续在客户端操作
				可以同时进行多个任务
				示例:
				显示所有后台任务
				BaiduPCS-Go bg
			`,
			Category: "其他",
			Before:   reloadFn,
			Action: func(c *cli.Context) error {
				if c.NArg() == 0 {
					pcscommand.BgMap.PrintAllBgTask()
					return nil
				}
				return nil
			},
			Flags: []cli.Flag{
				cli.BoolFlag{
					Name:  "test",
					Usage: "测试下载, 此操作不会保存文件到本地",
				},
			},
		},
		{
			Name:      "upload",
			Aliases:   []string{"u"},
			Usage:     "上传文件/目录",
			UsageText: app.Name + " upload <本地文件/目录的路径1> <文件/目录2> <文件/目录3> ... <目标目录>",
			Description: `
				上传默认采用分片上传的方式, 上传的文件将会保存到, <目标目录>.
				遇到同名文件将会自动覆盖!!
				当上传的文件名和网盘的目录名称相同时, 不会覆盖目录, 防止丢失数据.
				注意: 
				分片上传之后, 服务器可能会记录到错误的文件md5, 可使用 fixmd5 命令尝试修复文件的MD5值, 修复md5不一定能成功, 但文件的完整性是没问题的.
				fixmd5 命令使用方法:
				BaiduPCS-Go fixmd5 -h
				禁用分片上传可以保证服务器记录到正确的md5.
				禁用分片上传时只能使用单线程上传, 指定的单个文件上传最大线程数将会无效.
				示例:
				1. 将本地的 C:\Users\Administrator\Desktop\1.mp4 上传到网盘 /视频 目录
				注意区别反斜杠 "\" 和 斜杠 "/" !!!
				BaiduPCS-Go upload C:/Users/Administrator/Desktop/1.mp4 /视频
				2. 将本地的 C:\Users\Administrator\Desktop\1.mp4 和 C:\Users\Administrator\Desktop\2.mp4 上传到网盘 /视频 目录
				BaiduPCS-Go upload C:/Users/Administrator/Desktop/1.mp4 C:/Users/Administrator/Desktop/2.mp4 /视频
				3. 将本地的 C:\Users\Administrator\Desktop 整个目录上传到网盘 /视频 目录
				BaiduPCS-Go upload C:/Users/Administrator/Desktop /视频
				4. 使用相对路径
				BaiduPCS-Go upload 1.mp4 /视频
			`,
			Category: "百度网盘",
			Before:   reloadFn,
			Action: func(c *cli.Context) error {
				if c.NArg() < 2 {
					cli.ShowCommandHelp(c, c.Command.Name)
					return nil
				}

				subArgs := c.Args()
				pcscommand.RunUpload(subArgs[:c.NArg()-1], subArgs[c.NArg()-1], &pcscommand.UploadOptions{
					Parallel:       c.Int("p"),
					MaxRetry:       c.Int("retry"),
					NotRapidUpload: c.Bool("norapid"),
					NotSplitFile:   c.Bool("nosplit"),
				})
				return nil
			},
			Flags: []cli.Flag{
				cli.IntFlag{
					Name:  "p",
					Usage: "指定单个文件上传的最大线程数",
				},
				cli.IntFlag{
					Name:  "retry",
					Usage: "上传失败最大重试次数",
					Value: pcscommand.DefaultUploadMaxRetry,
				},
				cli.BoolFlag{
					Name:  "norapid",
					Usage: "不检测秒传",
				},
				cli.BoolFlag{
					Name:  "nosplit",
					Usage: "禁用分片上传",
				},
			},
		},
		{
			Name:      "locate",
			Aliases:   []string{"lt"},
			Usage:     "获取下载直链",
			UsageText: app.Name + " locate <文件1> <文件2> ...",
			Description: fmt.Sprintf(`
	获取下载直链

	若该功能无法正常使用, 提示"user is not authorized, hitcode:xxx", 尝试更换 User-Agent 为 %s:
	BaiduPCS-Go config set -user_agent "%s"
`, baidupcs.NetdiskUA, baidupcs.NetdiskUA),
			Category: "百度网盘",
			Before:   reloadFn,
			Action: func(c *cli.Context) error {
				if c.NArg() < 1 {
					cli.ShowCommandHelp(c, c.Command.Name)
					return nil
				}

				opt := &pcscommand.LocateDownloadOption{
					FromPan: c.Bool("pan"),
				}

				pcscommand.RunLocateDownload(c.Args(), opt)
				return nil
			},
			Flags: []cli.Flag{
				cli.BoolFlag{
					Name:  "pan",
					Usage: "从百度网盘首页获取下载链接",
				},
			},
		},
		{
			Name:      "rapidupload",
			Aliases:   []string{"ru"},
			Usage:     "手动秒传文件",
			UsageText: app.Name + " rapidupload -length=<文件的大小> -md5=<文件的md5值> -slicemd5=<文件前256KB切片的md5值(可选)> -crc32=<文件的crc32值(可选)> <保存的网盘路径, 需包含文件名>",
			Description: `
				使用此功能秒传文件, 前提是知道文件的大小, md5, 前256KB切片的 md5 (可选), crc32 (可选), 且百度网盘中存在一模一样的文件.
				上传的文件将会保存到网盘的目标目录.
				遇到同名文件将会自动覆盖! 
				可能无法秒传 20GB 以上的文件!!
				示例:
				1. 如果秒传成功, 则保存到网盘路径 /test
				BaiduPCS-Go rapidupload -length=56276137 -md5=fbe082d80e90f90f0fb1f94adbbcfa7f -slicemd5=38c6a75b0ec4499271d4ea38a667ab61 -crc32=314332359 /test
			`,
			Category: "百度网盘",
			Before:   reloadFn,
			Action: func(c *cli.Context) error {
				if c.NArg() <= 0 || !c.IsSet("md5") || !c.IsSet("length") || !c.IsSet("slicemd5") {
					cli.ShowCommandHelp(c, c.Command.Name)
					return nil
				}

				pcscommand.RunRapidUpload(c.Args().Get(0), c.String("md5"), c.String("slicemd5"), c.String("crc32"), c.Int64("length"))
				return nil
			},
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "md5",
					Usage: "文件的 md5 值",
				},
				cli.StringFlag{
					Name:  "slicemd5",
					Usage: "文件前 256KB 切片的 md5 值",
				},
				cli.StringFlag{
					Name:  "crc32",
					Usage: "文件的 crc32 值 (可选)",
				},
				cli.Int64Flag{
					Name:  "length",
					Usage: "文件的大小",
				},
			},
		},
		{
			Name:      "createsuperfile",
			Aliases:   []string{"csf"},
			Usage:     "手动分片上传—合并分片文件",
			UsageText: app.Name + " createsuperfile -path=<保存的网盘路径, 需包含文件名> block1 block2 ... ",
			Description: `
				block1, block2 ... 为文件分片的md5值
				上传的文件将会保存到网盘的目标目录.
				遇到同名文件将会自动覆盖! 
				示例:
				BaiduPCS-Go createsuperfile -path=1.mp4 ec87a838931d4d5d2e94a04644788a55 ec87a838931d4d5d2e94a04644788a55
			`,
			Category: "百度网盘",
			Before:   reloadFn,
			Action: func(c *cli.Context) error {
				if c.NArg() < 1 {
					cli.ShowCommandHelp(c, c.Command.Name)
					return nil
				}

				pcscommand.RunCreateSuperFile(c.String("path"), c.Args()...)
				return nil
			},
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "path",
					Usage: "保存的网盘路径",
					Value: "superfile",
				},
			},
		},
		{
			Name:      "fixmd5",
			Usage:     "修复文件MD5",
			UsageText: app.Name + " fixmd5 <文件1> <文件2> <文件3> ...",
			Description: `
				尝试修复文件的MD5值, 以便于校验文件的完整性和导出文件.
				使用分片上传文件, 当文件分片数大于1时, 百度网盘服务端最终计算所得的md5值和本地的不一致, 这可能是百度网盘的bug.
				不过把上传的文件下载到本地后，对比md5值是匹配的, 也就是文件在传输中没有发生损坏.
				对于MD5值可能有误的文件, 程序会在获取文件的元信息时, 给出MD5值 "可能不正确" 的提示, 表示此文件可以尝试进行MD5值修复.
				修复文件MD5不一定能成功, 原因可能是服务器未刷新, 可过几天后再尝试.
				修复文件MD5的原理为秒传文件, 即修复文件MD5成功后, 文件的创建日期, 修改日期, fs_id, 版本历史等信息将会被覆盖, 修复的MD5值将覆盖原先的MD5值, 但不影响文件的完整性.
				注意: 无法修复 20GB 以上文件的 md5!!
				示例:
				1. 修复 /我的资源/1.mp4 的 MD5 值
				BaiduPCS-Go fixmd5 /我的资源/1.mp4
			`,
			Category: "百度网盘",
			Before:   reloadFn,
			Action: func(c *cli.Context) error {
				if c.NArg() <= 0 {
					cli.ShowCommandHelp(c, c.Command.Name)
					return nil
				}

				pcscommand.RunFixMD5(c.Args()...)
				return nil
			},
		},
		{
			Name:      "sumfile",
			Aliases:   []string{"sf"},
			Usage:     "获取本地文件的秒传信息",
			UsageText: app.Name + " sumfile <本地文件的路径1> <本地文件的路径2> ...",
			Description: `
				获取本地文件的大小, md5, 前256KB切片的md5, crc32, 可用于秒传文件.
				示例:
				获取 C:\Users\Administrator\Desktop\1.mp4 的秒传信息
				BaiduPCS-Go sumfile C:/Users/Administrator/Desktop/1.mp4
			`,
			Category: "其他",
			Before:   reloadFn,
			Action: func(c *cli.Context) error {
				if c.NArg() <= 0 {
					cli.ShowCommandHelp(c, c.Command.Name)
					return nil
				}

				for k, filePath := range c.Args() {
					lp, err := checksum.GetFileSum(filePath, checksum.CHECKSUM_MD5|checksum.CHECKSUM_SLICE_MD5|checksum.CHECKSUM_CRC32)
					if err != nil {
						fmt.Printf("[%d] %s\n", k+1, err)
						continue
					}

					fmt.Printf("[%d] - [%s]:\n", k+1, filePath)

					strLength, strMd5, strSliceMd5, strCrc32 := strconv.FormatInt(lp.Length, 10), hex.EncodeToString(lp.MD5), hex.EncodeToString(lp.SliceMD5), strconv.FormatUint(uint64(lp.CRC32), 10)
					fileName := filepath.Base(filePath)

					tb := pcstable.NewTable(os.Stdout)
					tb.SetColumnAlignment([]int{tablewriter.ALIGN_LEFT, tablewriter.ALIGN_LEFT})
					tb.AppendBulk([][]string{
						[]string{"文件大小", strLength},
						[]string{"md5", strMd5},
						[]string{"前256KB切片的md5", strSliceMd5},
						[]string{"crc32", strCrc32},
						[]string{"秒传命令", app.Name + " rapidupload -length=" + strLength + " -md5=" + strMd5 + " -slicemd5=" + strSliceMd5 + " -crc32=" + strCrc32 + " " + fileName},
					})
					tb.Render()
					fmt.Printf("\n")
				}

				return nil
			},
		},
		{
			Name:      "share",
			Usage:     "分享文件/目录",
			UsageText: app.Name + " share",
			Category:  "百度网盘",
			Before:    reloadFn,
			Action: func(c *cli.Context) error {
				cli.ShowCommandHelp(c, c.Command.Name)
				return nil
			},
			Subcommands: []cli.Command{
				{
					Name:        "set",
					Aliases:     []string{"s"},
					Usage:       "设置分享文件/目录",
					UsageText:   app.Name + " share set <文件/目录1> <文件/目录2> ...",
					Description: `目前只支持创建私密链接.`,
					Action: func(c *cli.Context) error {
						if c.NArg() < 1 {
							cli.ShowCommandHelp(c, c.Command.Name)
							return nil
						}
						pcscommand.RunShareSet(c.Args(), nil)
						return nil
					},
				},
				{
					Name:      "list",
					Aliases:   []string{"l"},
					Usage:     "列出已分享文件/目录",
					UsageText: app.Name + " share list",
					Action: func(c *cli.Context) error {
						pcscommand.RunShareList(c.Int("page"))
						return nil
					},
					Flags: []cli.Flag{
						cli.IntFlag{
							Name:  "page",
							Usage: "分享列表的页数",
							Value: 1,
						},
					},
				},
				{
					Name:        "cancel",
					Aliases:     []string{"c"},
					Usage:       "取消分享文件/目录",
					UsageText:   app.Name + " share cancel <shareid_1> <shareid_2> ...",
					Description: `目前只支持通过分享id (shareid) 来取消分享.`,
					Action: func(c *cli.Context) error {
						if c.NArg() < 1 {
							cli.ShowCommandHelp(c, c.Command.Name)
							return nil
						}
						pcscommand.RunShareCancel(converter.SliceStringToInt64(c.Args()))
						return nil
					},
				},
			},
		},
		{
			Name:        "config",
			Usage:       "显示和修改程序配置项",
			Description: "显示和修改程序配置项",
			Category:    "配置",
			Before:      reloadFn,
			After:       saveFunc,
			Action: func(c *cli.Context) error {
				fmt.Printf("----\n运行 %s config set 可进行设置配置\n\n当前配置:\n", app.Name)
				pcsconfig.Config.PrintTable()
				return nil
			},
			Subcommands: []cli.Command{
				{
					Name:      "set",
					Usage:     "修改程序配置项",
					UsageText: app.Name + " config set [arguments...]",
					Description: `
	注意:
		可通过设置环境变量 BAIDUPCS_GO_CONFIG_DIR, 指定配置文件存放的目录.

		谨慎修改 appid, user_agent, pcs_ua, pan_ua 的值, 否则访问网盘服务器时, 可能会出现错误
		cache_size 的值支持可选设置单位了, 单位不区分大小写, b 和 B 均表示字节的意思, 如 64KB, 1MB, 32kb, 65536b, 65536
		max_upload_parallel, max_download_load 的值支持可选设置单位了, 单位为每秒的传输速率, 后缀'/s' 可省略, 如 2MB/s, 2MB, 2m, 2mb 均为一个意思

	例子:
		BaiduPCS-Go config set -appid=266719
		BaiduPCS-Go config set -enable_https=false
		BaiduPCS-Go config set -user_agent="netdisk;2.2.51.6;netdisk;10.0.63;PC;android-android"
		BaiduPCS-Go config set -cache_size 64KB
		BaiduPCS-Go config set -cache_size 16384 -max_parallel 200 -savedir D:/download`,
					Action: func(c *cli.Context) error {
						if c.NumFlags() <= 0 || c.NArg() > 0 {
							cli.ShowCommandHelp(c, c.Command.Name)
							return nil
						}

						if c.IsSet("appid") {
							pcsconfig.Config.SetAppID(c.Int("appid"))
						}
						if c.IsSet("enable_https") {
							pcsconfig.Config.SetEnableHTTPS(c.Bool("enable_https"))
						}
						if c.IsSet("user_agent") {
							pcsconfig.Config.SetUserAgent(c.String("user_agent"))
						}
						if c.IsSet("pcs_ua") {
							pcsconfig.Config.SetUserAgent(c.String("pcs_ua"))
						}
						if c.IsSet("pan_ua") {
							pcsconfig.Config.SetUserAgent(c.String("pan_ua"))
						}
						if c.IsSet("cache_size") {
							err := pcsconfig.Config.SetCacheSizeByStr(c.String("cache_size"))
							if err != nil {
								fmt.Printf("设置 cache_size 错误: %s\n", err)
								return nil
							}
						}
						if c.IsSet("max_parallel") {
							pcsconfig.Config.MaxParallel = c.Int("max_parallel")
						}
						if c.IsSet("max_upload_parallel") {
							pcsconfig.Config.MaxUploadParallel = c.Int("max_upload_parallel")
						}
						if c.IsSet("max_download_load") {
							pcsconfig.Config.MaxDownloadLoad = c.Int("max_download_load")
						}
						if c.IsSet("max_download_rate") {
							err := pcsconfig.Config.SetMaxDownloadRateByStr(c.String("max_download_rate"))
							if err != nil {
								fmt.Printf("设置 max_download_rate 错误: %s\n", err)
								return nil
							}
						}
						if c.IsSet("max_upload_rate") {
							err := pcsconfig.Config.SetMaxUploadRateByStr(c.String("max_upload_rate"))
							if err != nil {
								fmt.Printf("设置 max_upload_rate 错误: %s\n", err)
								return nil
							}
						}
						if c.IsSet("savedir") {
							pcsconfig.Config.SaveDir = c.String("savedir")
						}
						if c.IsSet("proxy") {
							pcsconfig.Config.SetProxy(c.String("proxy"))
						}
						if c.IsSet("local_addrs") {
							pcsconfig.Config.SetLocalAddrs(c.String("local_addrs"))
						}

						err := pcsconfig.Config.Save()
						if err != nil {
							fmt.Println(err)
							return err
						}

						pcsconfig.Config.PrintTable()
						fmt.Printf("\n保存配置成功!\n\n")

						return nil
					},
					Flags: []cli.Flag{
						cli.IntFlag{
							Name:  "appid",
							Usage: "百度 PCS 应用ID",
						},
						cli.StringFlag{
							Name:  "cache_size",
							Usage: "下载缓存",
						},
						cli.IntFlag{
							Name:  "max_parallel",
							Usage: "下载网络连接的最大并发量",
						},
						cli.IntFlag{
							Name:  "max_upload_parallel",
							Usage: "上传网络连接的最大并发量",
						},
						cli.IntFlag{
							Name:  "max_download_load",
							Usage: "同时进行下载文件的最大数量",
						},
						cli.StringFlag{
							Name:  "max_download_rate",
							Usage: "限制最大下载速度, 0代表不限制",
						},
						cli.StringFlag{
							Name:  "max_upload_rate",
							Usage: "限制最大上传速度, 0代表不限制",
						},
						cli.StringFlag{
							Name:  "savedir",
							Usage: "下载文件的储存目录",
						},
						cli.BoolFlag{
							Name:  "enable_https",
							Usage: "启用 https",
						},
						cli.StringFlag{
							Name:  "user_agent",
							Usage: "浏览器标识",
						},
						cli.StringFlag{
							Name:  "pcs_ua",
							Usage: "PCS 浏览器标识",
						},
						cli.StringFlag{
							Name:  "pan_ua",
							Usage: "Pan 浏览器标识",
						},
						cli.StringFlag{
							Name:  "proxy",
							Usage: "设置代理, 支持 http/socks5 代理",
						},
						cli.StringFlag{
							Name:  "local_addrs",
							Usage: "设置本地网卡地址, 多个地址用逗号隔开",
						},
					},
				},
			},
		},
	}

	app.Run(os.Args)
}
