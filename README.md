# BaiduPCS-WEB的后维护版本
请前往 https://github.com/Erope/BaiduPCS-Go

## 介绍

~~女生~~个人自用的BaiduPCS-WEB版本，添加了一些奇奇怪怪不稳定(或许根本用不了的)奇怪功能，不保证能用，不保证使用此软件不会导致地球💥，使用此软件一定不能获得高速下载

## 致谢

最初的版本: https://github.com/GangZhuo/BaiduPCS

后来的命令行维护者: https://github.com/iikira/

再后来的WEB版维护者: https://github.com/liuzhuoling2011/BaiduPCS-Go

我的贡献: 大概0.1%的代码不到

## 免责

即使您使用它真的导致了地球💥，我也不会对此负责...

## 使用方法

由于我不会写前端，因此后续的参数仅能通过命令行的方式直接初始化，而无法在前端显示并修改

由于是自用，因此并不提供编译完成的版本，但大概您直接`go build`也没有太大问题，配置好一些命令行工具后直接使用build.sh问题应该也不大

需要安装rice并把$GOPATH/bin目录加入到PATH中，请将最后的export命令放入bash等初始化文件中，方法是

```bash
go get github.com/GeertJohan/go.rice
go get github.com/GeertJohan/go.rice/rice
export PATH=$GOPATH/bin:$PATH
```

## 参数介绍

从某种程度讲，WEB版并没有删除命令行的功能，因此直接当它是命令行运行也是可行的，这也是为什么`-h`命令中会显示那么多参数命令的原因。

下面介绍WEB版本身的参数:

* --aria2, -a                      启用aria2下载，停用自带下载，最好搭配pdurl使用，否则可能出现403等情况，具体原因在下文的原理介绍中有
*  --aria2url value, --au value     aria2的url (default: "http://localhost:6800/jsonrpc")
* --aria2secret value, --as value  aria2-RPC的secret，默认为空
* --aria2pre value, --ap value     aria2-RPC添加下载链接时附加在前面的前缀，用于部分解决下载时的403问题和部分拉黑后无法下载问题，默认为空，注意，前缀是以Aria2服务器为起始点的!例子: http://localhost:5299/bd/，不保证能用
* --pdurl value, --pd value        使用 https://github.com/TkzcM/baiduwp 搭建的Pandownload搭建网站加速下载的网址，如 https://pandl.live/ ，注意需要输入开头的https或http和末尾的/，默认不使用，pandl.live已经不能使用了，建议几个朋友合起来整一个号搭建一个用，后期可能还会添加其他一些其他类似项目的支持(咕咕咕)

## 原理介绍

众所周知，大部分第三方支持程序都离不开优秀的安全研究人员，本项目也是如此。

在GangZhuo和iikira等等一众人的努力下，成功逆向出了Pan安卓版v7的API和部分网页端的API，包括rand的加密方式，但如你们所见，本项目现在已经无法进行高速下载了。

为什么?

因为v7的安卓版本身现在甚至都无法登陆，基于这个版本做的第三方程序又怎么可能正常运行?

换句话说，现在还能正常打开已经是谢天谢地了。

## Q/A

Q: 能不能xxx?

A: 不能.

Q: 分享的密码是什么?

A: 是`pass`

## 贡献者

https://github.com/waylonwang

https://github.com/juebanlin

## PS

**项目可能随时删库**

**不支持任何捐赠或类似捐赠的行为**
