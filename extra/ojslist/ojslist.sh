#!/bin/bash
#
# Turn OJS homepage into list of journal OAI endpoint candidates. Note that not
# all candidates will work (so sample manually).
#
# $ ojslist.sh URL
#
# Example:
#
# $ osjlist.sh https://www.aaai.org/ocs/index.php
#
# https:/www.aaai.org/ocs/index.php/AAAI/index/oai
# https:/www.aaai.org/ocs/index.php/HCOMP/index/oai
# https:/www.aaai.org/ocs/index.php/AIIDE/index/oai
# https:/www.aaai.org/ocs/index.php/IAAI/index/oai
# ...
#
# More sites to test:
#
# * https://publications.drdo.gov.in/ojs/index.php/
#
set -eu
set -o errexit -o pipefail -o nounset

command -v pup >/dev/null 2>&1 || {
	echo >&2 "https://github.com/ericchiang/pup required"
	exit 1
}

# -allow a command to fail with !’s side effect on errexit
# -use return value from ${PIPESTATUS[0]}, because ! hosed $?
! getopt --test >/dev/null
if [[ ${PIPESTATUS[0]} -ne 4 ]]; then
	echo 'I’m sorry, `getopt --test` failed in this environment.'
	exit 1
fi

OPTIONS=i
LONGOPTS=index

! PARSED=$(getopt --options=$OPTIONS --longoptions=$LONGOPTS --name "$0" -- "$@")
if [[ ${PIPESTATUS[0]} -ne 0 ]]; then
	# e.g. return value is 1
	#  then getopt has complained about wrong arguments to stdout
	exit 2
fi
# read getopt’s output this way to handle the quoting right:
eval set -- "$PARSED"

index=false
# now enjoy the options in order and nicely split until we see --
while true; do
	case "$1" in
	-i | --index)
		index=true
		shift
		;;
	--)
		shift
		break
		;;
	*)
		echo "Programming error"
		exit 3
		;;
	esac
done

# handle non-option arguments
if [[ $# -ne 1 ]]; then
	echo "$0: A single input file is required."
	exit 4
fi

OJSHOME=$1
tmp=$(mktemp)
curl -sL "$OJSHOME" >$tmp

if [ "$index" == true ]; then
	cat "$tmp" | pup 'a attr{href}' | egrep '/index.php/[^/]*/?$' | awk '{print $0"/index/oai"}' | sed -e 's@//index@/index@g' | sort -u
else
	cat "$tmp" | pup 'a attr{href}' | egrep '/index.php/[^/]*/?$' | awk '{print $0"/oai"}' | sed -e 's@//oai@/oai@g' | sort -u
fi
