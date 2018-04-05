#!/bin/sh

# This script bootstraps a VNC environment for new connections.
# It receives a port number as first argument on which the VNC
# server should listen.

function usage() {
cat <<-EOF
USAGE: $(basename $0) [OPTIONS] [PORT] [PORTV6]

OPTIONS

-h  Display help message
EOF
}

while [[ ${1:0:1} == - ]]; do
  [[ $1 =~ ^-h|--help ]] && {
    usage
    exit;
  };

  [[ $1 == -- ]] && { shift; break; };

  break;
done

if [ "$#" -lt 2 ]; then
  usage
  exit
fi

PORT=$1
PORTV6=$2

DISPLAY=""

FBFILE=$(mktemp /tmp/.vncbootstrap-fb-XXXXXX)
AUTHSOCKET=$(mktemp /tmp/.vncbootstrap-auth-XXXXXX)
LOGFILE=$(mktemp /var/log/vncd-XXXXXX.log)

# Start X Server
exec 6<> ${FBFILE}
/usr/bin/X -displayfd 6 -auth ${AUTHSOCKET} &
exec 6<&- # close file

while [[ ${DISPLAY} == "" ]]; do
  DISPLAY=$(cat ${FBFILE})
done
echo "X server display: ${DISPLAY}"

# Start VNC Server
/usr/bin/x11vnc -xkb -noxrecord -noxfixes -noxdamage -rfbport ${PORT} -rfbportv6 ${PORTV6} \
                -display :${DISPLAY} -auth ${AUTHSOCKET} -ncache 10 -o ${LOGFILE}
