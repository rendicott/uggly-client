version := 0.0.8
projectName := ugglyc
packageNameNix := $(projectName)-linux-amd64-$(version).tar.gz
packageNameMac := $(projectName)-darwin-amd64-$(version).tar.gz
packageNameWindows := $(projectName)-windows-amd64-$(version).tar.gz

build_dir := output
build_dir_linux := output-linux
build_dir_mac := output-mac
build_dir_windows := output-windows

build: format configure build-linux build-mac build-windows
dev: format configure build-linux test
devd: format configure build-linux test-delete
dig: format configure build-linux test-delete

format:
	go fmt ./...


configure:
		mkdir -p $(build_dir)
		mkdir -p $(build_dir_linux)
		mkdir -p $(build_dir_mac)
		mkdir -p $(build_dir_windows)


build-linux:
		env GOOS=linux GOARCH=amd64 go build -o ./$(build_dir_linux)/$(projectName) -ldflags "-X main.version=$(version)"
		@cd ./$(build_dir_linux) && tar zcf ../$(build_dir)/$(packageNameNix) . 

build-mac:
		env GOOS=darwin GOARCH=amd64 go build -o ./$(build_dir_mac)/$(projectName) -ldflags "-X main.version=$(version)"
		@cd ./$(build_dir_mac) && tar zcf ../$(build_dir)/$(packageNameMac) . 

build-windows:
		env GOOS=windows GOARCH=amd64 go build -o ./$(build_dir_windows)/$(projectName).exe -ldflags "-X main.version=$(version)"
		@cd ./$(build_dir_windows) && tar zcf ../$(build_dir)/$(packageNameWindows) . 

clean:
		rm -rf $(build_dir)
		rm -rf $(build_dir_linux)
		rm -rf $(build_dir_mac)
		rm -rf $(build_dir_windows)		 

release:
	gh release create v$(version) ./output/*$(version).tar.gz
