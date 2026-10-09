package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
)

//go:embed index.html
var indexHTML string

var validPassword = regexp.MustCompile(`^[a-zA-Z0-9]+$`)

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintf(os.Stderr, "用法: %s <用户名> <密码> <端口>\n", os.Args[0])
		os.Exit(1)
	}
	username := os.Args[1]
	password := os.Args[2]
	port := os.Args[3]

	if !strings.HasPrefix(port, ":") {
		port = ":" + port
	}

	storageDir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "获取当前目录失败: %v\n", err)
		os.Exit(1)
	}

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()

	r.Use(gin.Logger())
	r.Use(gin.Recovery())

	r.Use(func(c *gin.Context) {
		if c.Request.URL.Path == "/" {
			c.Next()
			return
		}
		auth := c.Request.Header.Get("Authorization")
		if auth == "" {
			c.Header("WWW-Authenticate", `Basic realm="File Manager"`)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "未授权"})
			return
		}
		parts := strings.SplitN(auth, " ", 2)
		if len(parts) != 2 || parts[0] != "Basic" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "无效的认证格式"})
			return
		}
		decode, err := base64.StdEncoding.DecodeString(parts[1])
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "认证解码失败"})
			return
		}
		pair := strings.SplitN(string(decode), ":", 2)
		if len(pair) != 2 || pair[0] != username || pair[1] != password {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
			return
		}
		c.Next()
	})

	allowedPrefix := storageDir + string(os.PathSeparator)

	safeRelPath := func(raw string) (string, bool) {
		if raw == "" {
			return "", true
		}
		p := filepath.ToSlash(filepath.Clean(raw))
		if p == "." {
			return "", true
		}
		if p == ".." || strings.HasPrefix(p, "../") || filepath.IsAbs(raw) {
			return "", false
		}
		if !strings.HasPrefix(filepath.Join(storageDir, p), allowedPrefix) {
			return "", false
		}
		return p, true
	}

	listFiles := func(c *gin.Context) {
		relDir, ok := safeRelPath(c.Query("path"))
		if !ok {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "访问路径被拒绝"})
			return
		}
		dirPath := storageDir
		if relDir != "" {
			dirPath = filepath.Join(storageDir, relDir)
		}
		info, err := os.Stat(dirPath)
		if err != nil || !info.IsDir() {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "目录不存在"})
			return
		}
		files, err := os.ReadDir(dirPath)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "读取目录失败"})
			return
		}
		var fileList []gin.H
		for _, f := range files {
			if f.IsDir() {
				fileList = append(fileList, gin.H{"name": f.Name(), "isDir": true, "size": 0})
			} else {
				info, _ := f.Info()
				fileList = append(fileList, gin.H{"name": f.Name(), "isDir": false, "size": info.Size()})
			}
		}
		c.JSON(http.StatusOK, gin.H{"files": fileList})
	}

	downloadFile := func(c *gin.Context) {
		relPath := c.Param("path")
		fullPath := filepath.Join(storageDir, relPath)
		if !strings.HasPrefix(fullPath, allowedPrefix) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "访问外部路径被拒绝"})
			return
		}
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "文件不存在"})
			return
		}
		c.File(fullPath)
	}

	uploadFile := func(c *gin.Context) {
		file, handler, err := c.Request.FormFile("file")
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "获取文件失败"})
			return
		}
		defer file.Close()

		relDir, ok := safeRelPath(c.PostForm("path"))
		if !ok {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "保存路径被拒绝"})
			return
		}
		destPath := filepath.Join(storageDir, relDir, handler.Filename)
		if !strings.HasPrefix(destPath, allowedPrefix) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "保存路径被拒绝"})
			return
		}

		out, err := os.Create(destPath)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "创建文件失败"})
			return
		}
		defer out.Close()

		io.Copy(out, file)
		c.JSON(http.StatusOK, gin.H{"message": "上传成功", "filename": handler.Filename})
	}

	deleteFile := func(c *gin.Context) {
		relPath := c.Param("path")
		fullPath := filepath.Join(storageDir, relPath)
		if !strings.HasPrefix(fullPath, allowedPrefix) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "删除路径被拒绝"})
			return
		}
		info, err := os.Stat(fullPath)
		if err != nil {
			if os.IsNotExist(err) {
				c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "文件不存在"})
				return
			}
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "删除失败"})
			return
		}
		if info.IsDir() {
			err = os.RemoveAll(fullPath)
		} else {
			err = os.Remove(fullPath)
		}
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "删除失败"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
	}

	compressDirectory := func(c *gin.Context) {
		relDir, ok := safeRelPath(c.Query("path"))
		if !ok {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "压缩路径被拒绝"})
			return
		}
		root := storageDir
		if relDir != "" {
			root = filepath.Join(storageDir, relDir)
		}
		rootInfo, err := os.Stat(root)
		if err != nil || !rootInfo.IsDir() {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "目录不存在"})
			return
		}
		tarPath := filepath.Join(storageDir, "archive.tar.gz")
		out, err := os.Create(tarPath)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "创建压缩文件失败"})
			return
		}

		gzWriter := gzip.NewWriter(out)
		tarWriter := tar.NewWriter(gzWriter)

		closeAll := func() error {
			err1 := tarWriter.Close()
			err2 := gzWriter.Close()
			err3 := out.Close()
			if err1 != nil {
				return err1
			}
			if err2 != nil {
				return err2
			}
			return err3
		}

		walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if path == tarPath {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			name := filepath.ToSlash(rel)
			if d.IsDir() {
				if name == "." {
					return nil
				}
				name += "/"
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			linkname := ""
			if info.Mode()&os.ModeSymlink != 0 {
				linkname, err = os.Readlink(path)
				if err != nil {
					return err
				}
			}
			header, err := tar.FileInfoHeader(info, linkname)
			if err != nil {
				return err
			}
			header.Name = name
			if err := tarWriter.WriteHeader(header); err != nil {
				return err
			}
			if info.Mode().IsRegular() {
				file, err := os.Open(path)
				if err != nil {
					return err
				}
				defer file.Close()
				if _, err := io.Copy(tarWriter, file); err != nil {
					return err
				}
			}
			return nil
		})

		if walkErr == nil {
			walkErr = closeAll()
		} else {
			out.Close()
		}

		if walkErr != nil {
			os.Remove(tarPath)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "压缩失败: " + walkErr.Error()})
			return
		}

		c.FileAttachment(tarPath, "archive.tar.gz")
		os.Remove(tarPath)
	}

	extractArchive := func(c *gin.Context) {
		file, _, err := c.Request.FormFile("archive")
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "获取归档文件失败"})
			return
		}
		defer file.Close()

		data, err := io.ReadAll(file)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "读取归档文件失败"})
			return
		}

		gzReader, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "无法解压 gzip 文件"})
			return
		}
		defer gzReader.Close()

		relDir, ok := safeRelPath(c.PostForm("path"))
		if !ok {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "提取路径被拒绝"})
			return
		}

		tarReader := tar.NewReader(gzReader)

		for {
			header, err := tarReader.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "归档文件损坏"})
				return
			}

			name := filepath.Clean(filepath.ToSlash(header.Name))
			if name == "." || name == ".." || strings.HasPrefix(name, "../") || filepath.IsAbs(name) {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "提取路径被拒绝"})
				return
			}
			targetPath := filepath.Join(storageDir, relDir, name)
			if !strings.HasPrefix(targetPath, allowedPrefix) {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "提取路径被拒绝"})
				return
			}

			if header.Typeflag == tar.TypeDir {
				if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
					c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "创建目录失败"})
					return
				}
				continue
			}

			if header.Typeflag != tar.TypeReg {
				continue
			}

			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "创建目录失败"})
				return
			}

			mode := os.FileMode(header.Mode)
			if mode == 0 {
				mode = 0644
			}
			outFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "创建文件失败"})
				return
			}
			if _, err := io.Copy(outFile, tarReader); err != nil {
				outFile.Close()
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "写入文件失败"})
				return
			}
			outFile.Close()
		}

		c.JSON(http.StatusOK, gin.H{"message": "解压成功"})
	}

	changePassword := func(c *gin.Context) {
		var req struct {
			OldPassword string `json:"old_password"`
			NewPassword string `json:"new_password"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
			return
		}
		if req.OldPassword != password {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "旧密码错误"})
			return
		}
		if !validPassword.MatchString(req.NewPassword) {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "新密码只能包含字母和数字"})
			return
		}
		password = req.NewPassword
		c.JSON(http.StatusOK, gin.H{"message": "密码修改成功"})
	}

	r.GET("/", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(indexHTML))
	})
	r.GET("/files", listFiles)
	r.GET("/download/*path", downloadFile)
	r.POST("/upload", uploadFile)
	r.DELETE("/delete/*path", deleteFile)
	r.POST("/compress", compressDirectory)
	r.POST("/extract", extractArchive)
	r.POST("/password", changePassword)

	fmt.Printf("文件管理器启动: http://127.0.0.1%s (用户: %s)\n", port, username)
	if err := r.Run(port); err != nil {
		fmt.Fprintf(os.Stderr, "服务器启动失败: %v\n", err)
		os.Exit(1)
	}
}
