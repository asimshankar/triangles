# triangles
* [![Build Status](https://travis-ci.org/asimshankar/triangles.svg)](https://travis-ci.org/asimshankar/triangles)
* [Go](https://golang.org)+[v23](https://github.com/vanadium/go.v23)+[Mobile](https://github.com/golang/mobile)
* Heavily inspired by [volley](https://github.com/monopole/volley), though this
  utilizes the [v23](https://github.com/vanadium/go.v23) [network discovery
  API](https://godoc.org/v.io/v23/discovery) to simplify setup
* For more information on v23, sign up [here](https://v.io)

# Quick Start

# Linux
```
sudo apt-get install libegl1-mesa-dev libgles2-mesa-dev libx11-dev  #  https://github.com/golang/mobile/blob/master/app/x11.go#L15
go get github.com/asimshankar/triangles
$GOPATH/bin/triangles --logtostderr
```

# OS X
```
go get github.com/asimshankar/triangles
$GOPATH/bin/triangles  --logtostderr
```

# Android
Requires the Android SDK to be installed. Easiest way is to install [Android
Studio](https://developer.android.com/sdk/index.html). After that:
```
# Connect the Android device via USB
go get golang.org/x/mobile/cmd/gomobile
gomobile init
go get github.com/asimshankar/triangles
gomobile install github.com/asimshankar/triangles
# And see the application's logs.
adb logcat | grep GoLog
```

# iOS
* Become an [Apple app developer](https://developer.apple.com/programs) (get an apple ID, device auth, etc.)
* Install [XCode](https://developer.apple.com/xcode/download/), perhaps: `xcode-select --install`
* Get [git](http://git-scm.com/download/mac)
* Build the mobile app
```
go get golang.org/x/mobile/cmd/gomobile
gomobile init
go get github.com/asimshankar/triangles
gomobile build --target=ios github.com/asimshankar/triangles
```
This will generate `triangles.app`
* Start [XCode](https://developer.apple.com/xcode/download/)
* Plugin an iOS device to your USB port
* Navigate the XCode menus: Window --> Devices
* Select the device and then click the `+` icon on _Installed Applications_ to select `triangles.app`
