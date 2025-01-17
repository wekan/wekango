#!/bin/bash

echo "Note: Console output is also logged to ../wekan-log.txt"

function pause(){
	read -p "$*"
}

echo
PS3='Please enter your choice: '
options=("Install Wekan dependencies" "Build Wekan" "Run on http://localhost:4000" "Quit")

select opt in "${options[@]}"
do
    case $opt in
        "Install Wekan dependencies")

		if [[ "$OSTYPE" == "linux-gnu" ]]; then
			echo "Linux";
			# Debian, Ubuntu, Mint
			sudo apt install -y golang build-essential gcc g++ make git curl wget p7zip-full zip unzip unp npm p7zip-full
		elif [[ "$OSTYPE" == "darwin"* ]]; then
		        echo "macOS";
			brew install go
		elif [[ "$OSTYPE" == "win32" ]]; then
		        # Windows
		        choco install -y golang
			exit;
		elif [[ "$OSTYPE" == "freebsd"* ]]; then
		        echo "TODO: Add FreeBSD";
			exit;
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

    "Run on http://localhost:4000")
		#Not in use, could increase RAM usage: NODE_OPTIONS="--max_old_space_size=4096"
		#---------------------------------------------------------------------
		# Logging of terminal output to console and to ../wekan-log.txt at end of this line: 2>&1 | tee ../wekan-log.txt
		#WARN_WHEN_USING_OLD_API=true NODE_OPTIONS="--trace-warnings"
		WRITABLE_PATH=.. WITH_API=true RICHER_CARD_COMMENT_EDITOR=false ROOT_URL=http://localhost:4000 PORT=4000 ./wekango 2>&1 | tee ../wekan-log.txt
		#---------------------------------------------------------------------
		break
		;;

    "Quit")
		break
		;;
    *) echo invalid option;;
    esac
done
