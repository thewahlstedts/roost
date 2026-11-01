PREFIX ?= $(HOME)/.local/bin

roost: *.go go.mod
	go build -o roost .

install: roost
	mkdir -p $(PREFIX)
	rm -f $(PREFIX)/roost  # replace, never overwrite in place (macOS kills overwritten signed binaries)
	cp roost $(PREFIX)/roost
	@echo "Installed to $(PREFIX)/roost — now run: roost setup"

clean:
	rm -f roost

.PHONY: install clean
