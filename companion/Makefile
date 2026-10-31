APP := MiniPulseMenu.app
BIN := .build/release/MiniPulseMenu

.PHONY: build test app clean

build:
	swift build -c release

test:
	swift test

# Assemble a runnable agent-app bundle. Ad-hoc signed: there is no
# Developer ID identity on the release machines yet, matching the
# Go daemon's current unsigned distribution (see repo README).
app: build
	rm -rf $(APP)
	mkdir -p $(APP)/Contents/MacOS
	cp $(BIN) $(APP)/Contents/MacOS/MiniPulseMenu
	cp Resources/Info.plist $(APP)/Contents/Info.plist
	codesign --force --sign - $(APP)

clean:
	rm -rf .build $(APP)
