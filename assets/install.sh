#!/usr/bin/env bash

# This script will install vncd in default locations on a linux systems
# Run as root

cp vncd /sbin/vncd
mkdir -p /etc/vncd
cp startvnc.sh /etc/vncd
