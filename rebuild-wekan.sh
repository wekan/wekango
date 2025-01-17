#!/bin/bash

echo "Note: Console output is also logged to ../wekan-log.txt"

function pause(){
	read -p "$*"
}

echo
PS3='Please enter your choice: '
options=("Install Wekan dependencies" "Build Wekan" "Run on http://localhost:8000" "Build for all platforms to .build directory" "Quit")

select opt in "${options[@]}"
do
    case $opt in
        "Install Wekan dependencies")

		if [[ "$OSTYPE" == "linux-gnu" ]]; then
			echo "Linux";
			# Debian, Ubuntu, Mint
			for pkg in golang build-essential gcc g++ make git curl wget p7zip-full zip unzip unp npm; do
				if ! dpkg -l | grep -q "^ii  $pkg "; then
					sudo apt install -y $pkg
				else
					echo "$pkg is already installed"
				fi
			done
			
			# Install cross-compilation tools
			for pkg in gcc-multilib g++-multilib gcc-arm-linux-gnueabi g++-arm-linux-gnueabi gcc-aarch64-linux-gnu g++-aarch64-linux-gnu mingw-w64 gcc-mingw-w64 g++-mingw-w64 \
				gcc-riscv64-linux-gnu g++-riscv64-linux-gnu \
				gcc-powerpc64-linux-gnu g++-powerpc64-linux-gnu \
				gcc-powerpc64le-linux-gnu g++-powerpc64le-linux-gnu \
				gcc-s390x-linux-gnu g++-s390x-linux-gnu \
				gcc-mips64-linux-gnuabi64 g++-mips64-linux-gnuabi64 \
				gcc-mips64el-linux-gnuabi64 g++-mips64el-linux-gnuabi64; do
				if ! dpkg -l | grep -q "^ii  $pkg "; then
					sudo apt install -y $pkg
				else
					echo "$pkg is already installed"
				fi
			done
			
			# Add 32-bit development libraries
			for pkg in libc6-dev-i386 linux-libc-dev:i386; do
				if ! dpkg -l | grep -q "^ii  $pkg "; then
					sudo apt install -y $pkg
				else
					echo "$pkg is already installed"
				fi
			done

			# Install Android NDK
            if [ ! -d "/usr/local/android-ndk-r25c" ]; then
                echo "Installing Android NDK..."
                wget https://dl.google.com/android/repository/android-ndk-r25c-linux.zip
                unzip android-ndk-r25c-linux.zip
                sudo mv android-ndk-r25c /usr/local/
                echo 'export ANDROID_NDK_HOME=/usr/local/android-ndk-r25c' >> ~/.bashrc
                source ~/.bashrc
            else
                echo "Android NDK already installed"
            fi

		elif [[ "$OSTYPE" == "darwin"* ]]; then
		        echo "macOS";
			# Check and install go
			if ! command -v go >/dev/null 2>&1; then
				brew install go
			else
				echo "go is already installed"
			fi
			
			# Check and install mingw-w64
			if ! brew list mingw-w64 &>/dev/null; then
				brew install mingw-w64
			else
				echo "mingw-w64 is already installed"
			fi
			
			# Check Android NDK
            if ! brew list android-ndk &>/dev/null; then
                brew install android-ndk
                echo 'export ANDROID_NDK_HOME=/usr/local/share/android-ndk' >> ~/.zshrc
                source ~/.zshrc
            else
                echo "Android NDK already installed"
            fi

		elif [[ "$OSTYPE" == "win32" ]]; then
		    # Windows
		    for pkg in golang mingw; do
				if ! choco list --local-only $pkg | grep -q "^$pkg "; then
					choco install -y $pkg
				else
					echo "$pkg is already installed"
				fi
			done
			
			# Check Android NDK
            if ! choco list --local-only android-ndk | grep -q "android-ndk"; then
                choco install -y androidstudio
                choco install -y android-ndk
                echo 'export ANDROID_NDK_HOME="C:/Android/android-ndk"' >> ~/.bashrc
                source ~/.bashrc
            else
                echo "Android NDK already installed"
            fi

		elif [[ "$OSTYPE" == "freebsd"* ]]; then
		        echo "Installing FreeBSD dependencies";
                for pkg in go gcc gmake git curl wget p7zip zip unzip npm base-devel llvm clang; do
				if ! pkg info -e $pkg >/dev/null 2>&1; then
					sudo pkg install -y $pkg
				else
					echo "$pkg is already installed"
				fi
			done
                break;
        elif [[ "$OSTYPE" == "dragonfly"* ]]; then
                echo "Installing DragonFly BSD dependencies";
                for pkg in go gcc gmake git curl wget p7zip zip unzip npm base-devel llvm clang; do
				if ! pkg info -e $pkg >/dev/null 2>&1; then
					sudo pkg install -y $pkg
				else
					echo "$pkg is already installed"
				fi
			done
                break;
		else
		        echo "Unknown"
			echo ${OSTYPE}
			exit;
		fi

		break
		;;

    "Build Wekan")
		echo "Building Wekan."
                # Updating dependencies
                go get -u
                # Tidying
                go mod tidy
                # Building
                go build
		echo Done.
		break
		;;

    "Run on http://localhost:8000")
		WRITABLE_PATH=.. WITH_API=true RICHER_CARD_COMMENT_EDITOR=false ROOT_URL=http://localhost:8000 PORT=8000 ./wekango 2>&1 | tee ../wekan-log.txt
		#---------------------------------------------------------------------
		break
		;;

	"Build for all platforms to .build directory")
		# Building for all platforms in parallel
		rm -rf .build
		mkdir -p .build
		
		if [[ "$OSTYPE" == "darwin"* ]]; then
			# On macOS, include BSD platforms along with well-supported platforms
            platforms=$(go tool dist list | grep -E "^(darwin|linux|windows)/(amd64|arm64)")
            # Add Raspberry Pi targets
            platforms="$platforms
linux/arm
linux/arm64"
		elif [[ "$OSTYPE" == "freebsd"* || "$OSTYPE" == "dragonfly"* ]]; then
			# On BSD, build for all BSD platforms plus Darwin
			platforms=$(go tool dist list | grep -E "^(freebsd|openbsd|netbsd|dragonfly|darwin)/(amd64|arm64)")
		else
			# On Linux and other platforms, exclude BSD platforms and add Raspberry Pi targets
            platforms=$(go tool dist list | grep -E "^(linux|windows|darwin)/(amd64|arm64|386)" | grep -v -E "^(solaris|plan9|js|android|ios|freebsd|openbsd|netbsd|dragonfly)")
            # Add Raspberry Pi targets and s390x
            platforms="$platforms
linux/arm
linux/arm64
linux/s390x"
		fi
		
		build_platform() {
			local platform=$1
			IFS="/" read -r GOOS GOARCH <<< "$platform"
			output_name=".build/wekan-$GOOS-$GOARCH"
			
			# Add Raspberry Pi specific naming
			if [ "$GOOS" = "linux" ]; then
				case "$GOARCH" in
					"arm")
						output_name=".build/wekan-raspberrypi-armhf"
						;;
					"arm64")
						output_name=".build/wekan-raspberrypi-arm64"
						;;
				esac
			fi
			
			if [ "$GOOS" = "windows" ]; then
				output_name+='.exe'
			fi

			echo "Building for $GOOS/$GOARCH..."
			
			# Reset environment variables
			unset CC CXX CGO_CFLAGS CGO_LDFLAGS CFLAGS CXXFLAGS LDFLAGS
			export CGO_ENABLED=1

			case "$GOOS" in
					"linux")
					case "$GOARCH" in
						"arm")
							export CC="arm-linux-gnueabi-gcc"
							export CXX="arm-linux-gnueabi-g++"
							export GOARM=6
							export CGO_CFLAGS="-march=armv6 -marm -mfpu=vfp -mfloat-abi=softfp"
							export CGO_LDFLAGS="-march=armv6 -marm -mfpu=vfp -mfloat-abi=softfp"
							;;
						"arm64")
							export CC="aarch64-linux-gnu-gcc"
							export CXX="aarch64-linux-gnu-g++"
							;;
						"386")
							export CC="gcc -m32"
							export CXX="g++ -m32"
							;;
						"s390x")
							export CC="s390x-linux-gnu-gcc"
							export CXX="s390x-linux-gnu-g++"
							;;
						*)
							export CC="gcc"
							export CXX="g++"
							;;
					esac
					;;
				"freebsd"|"openbsd"|"netbsd"|"dragonfly")
					if ! command -v clang &> /dev/null; then
						echo "Error: clang is required for BSD builds. Skipping $GOOS/$GOARCH"
						return 1
					fi
					export CC="clang"
					export CXX="clang++"
					;;
				"windows")
					if [ "$GOARCH" = "386" ]; then
						export CC="i686-w64-mingw32-gcc"
						export CXX="i686-w64-mingw32-g++"
					else
						export CC="x86_64-w64-mingw32-gcc"
						export CXX="x86_64-w64-mingw32-g++"
					fi
					;;
			esac

			env GOOS=$GOOS GOARCH=$GOARCH go build -o $output_name
			if [ $? -ne 0 ]; then
				echo "An error occurred while building for $GOOS/$GOARCH"
				return 1
			fi
		}
		
		export -f build_platform
		
		# Use GNU Parallel to build in parallel
		if command -v parallel >/dev/null 2>&1; then
			echo "$platforms" | parallel build_platform
		else
			# Fallback to sequential build if parallel is not available
			for platform in $platforms; do
				build_platform "$platform"
			done
		fi
		
		echo "Done."
		break
		;;

    "Quit")
		break
		;;
    *) echo invalid option;;
    esac
done
