# 文件管理器 (File Manager)

一个基于 Go + Gin 的轻量级 Web 文件管理器，内置网页界面，可对运行目录下的文件进行浏览、上传、下载、删除、压缩与解压，并支持 Basic Auth 鉴权与在线修改密码。

## 功能特性

- 网页端登录（HTTP Basic Auth 鉴权）
- 浏览当前工作目录及子目录
- 上传文件 / 下载文件
- 删除文件或目录
- 将目录压缩为 `archive.tar.gz` 并下载
- 上传 `tar.gz` 归档并解压到指定目录
- 在线修改登录密码（仅限字母与数字）

## 环境要求

- Go 1.25 及以上

## 构建与运行

### 1. 下载依赖

```bash
go mod tidy
```

### 2. 编译

```bash
go build -o filemanager .
```

### 3. 运行

程序需要 3 个命令行参数：**用户名**、**密码**、**端口**。

```bash
./filemanager <用户名> <密码> <端口>
```

示例：

```bash
./filemanager admin 123456 8080
```

启动后会输出类似：

```
文件管理器启动: http://127.0.0.1:8080 (用户: admin)
```

> 端口参数可带或不带前缀 `:`（例如 `8080` 或 `:8080` 均可）。

### 直接以源码运行

```bash
go run . <用户名> <密码> <端口>
```

## 使用说明

1. 在浏览器中打开启动地址（如 `http://127.0.0.1:8080`）。
2. 在登录页输入启动时的**用户名**和**密码**。
3. 登录后即可在网页端进行以下操作：
   - **上传文件**：将文件上传到当前目录。
   - **解压归档**：上传 `.tar.gz` 文件并解压到当前目录。
   - **压缩并下载**：将当前目录打包为 `archive.tar.gz` 并下载。
   - **刷新**：重新加载文件列表。
   - **修改密码**：修改登录密码（只能包含字母和数字）。
   - **退出登录**：退出当前账号。
   - 点击目录的「进入」可进入子目录，点击「返回上级」可回到上一层。
   - 文件支持「下载」与「删除」，目录支持「删除」。

> **注意**：文件管理器以**启动命令所在的工作目录**为根目录，所有文件操作都限制在该目录内（已做路径穿越防护）。

## HTTP API

除根路径 `/` 外，所有 API 均需通过 `Authorization: Basic <base64(用户名:密码)>` 请求头鉴权。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/` | 返回内置网页界面 |
| GET | `/files?path=<相对路径>` | 列出目录内容（默认当前目录） |
| GET | `/download/*path` | 下载指定文件 |
| POST | `/upload` | 上传文件，`form-data` 字段：`file`（文件）、`path`（目标相对目录） |
| DELETE | `/delete/*path` | 删除文件或目录 |
| POST | `/compress?path=<相对路径>` | 将目录压缩为 `archive.tar.gz` 并返回下载 |
| POST | `/extract` | 解压归档，`form-data` 字段：`archive`（`.tar.gz` 文件）、`path`（目标相对目录） |
| POST | `/password` | 修改密码，JSON 请求体：`{"old_password": "...", "new_password": "..."}` |

### API 调用示例

获取目录列表（需鉴权）：

```bash
curl -u admin:123456 http://127.0.0.1:8080/files
```

上传文件：

```bash
curl -u admin:123456 -F "file=@/path/to/local.txt" -F "path=" http://127.0.0.1:8080/upload
```

下载文件：

```bash
curl -u admin:123456 -O http://127.0.0.1:8080/download/somefile.txt
```

删除文件：

```bash
curl -u admin:123456 -X DELETE http://127.0.0.1:8080/delete/somefile.txt
```

修改密码：

```bash
curl -u admin:123456 -X POST -H "Content-Type: application/json" \
  -d '{"old_password":"123456","new_password":"abc123"}' \
  http://127.0.0.1:8080/password
```

## 安全说明

- 密码以明文参数传入命令行，可能通过进程列表（`ps`）等方式被泄露，建议仅在可信环境中使用。
- 当前鉴权为 HTTP Basic Auth，传输未加密，建议配合 HTTPS 反向代理使用。
- 新密码仅允许字母和数字组合，以降低注入风险。
