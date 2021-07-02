package pcsweb

import (
	"fmt"
	"github.com/Erope/BaiduPCS-Go/internal/pcsconfig"
	"net/http"
)

func middleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")             //允许访问所有域
		w.Header().Add("Access-Control-Allow-Headers", "Content-Type") //header的类型
		next.ServeHTTP(w, r)
	}
}

func activeAuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	next2 := middleware(next)

	return func(w http.ResponseWriter, r *http.Request) {
		err := pcsconfig.Config.Reload()
		if err != nil {
			fmt.Printf("重载配置错误: %s\n", err)
		}

		if GlobalSessions == nil || GlobalSessions.CheckLock(w, r) {
			response := &Response{
				Code: NeedPass,
				Msg:  "Pease unlock first!",
			}
			w.Header().Set("Access-Control-Allow-Origin", "*")             //允许访问所有域
			w.Header().Add("Access-Control-Allow-Headers", "Content-Type") //header的类型
			w.Write(response.JSON())
			return
		}

		activeUser := pcsconfig.Config.ActiveUser()

		if activeUser.Name == "" {
			response := &Response{
				Code: NotLogin,
				Msg:  "Pease login first!",
			}
			w.Header().Set("Access-Control-Allow-Origin", "*")             //允许访问所有域
			w.Header().Add("Access-Control-Allow-Headers", "Content-Type") //header的类型
			w.Write(response.JSON())
		} else {
			next2.ServeHTTP(w, r)
		}
	}
}

// rootMiddleware 根目录中间件
func rootMiddleware(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" {
		// 跳转到 /index.html
		//w.Header().Set("Location", "/index.html")
		//http.Error(w, "", 301)
		indexPage(w, r)
	} else {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(404)

		//tmpl := boxTmplParse("index", "index.html", "404.html")
		//checkErr(tmpl.Execute(w, nil))
	}
}
