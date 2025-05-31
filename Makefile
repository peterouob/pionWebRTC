# 如果要執行go檔案
# 1. 先把main.go中的port改變 8080通常被佔
# 2. 刪掉go.mod & go.sum

# 3. 執行
pkg:
	go mod init wbrtc

# 4. 安裝library
install:
	go mod tidy

# 5. 執行
run:
	go run main.go