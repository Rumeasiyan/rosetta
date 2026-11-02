#!/bin/sh
# Writes the plug-in's scripts/langTool.js for a run, from the file as fetched.
#   plugin.sh fetch <dir>      fetch ONLYOFFICE's source into <dir>, outside the repository
#   plugin.sh A <dir>          stock, addresses edited
#   plugin.sh B <dir>          A, plus credentials on: the whole of the edit an installer makes
set -eu
SERVICE=https://rosetta.epc.test
case "$1" in
fetch)
	fetch() {
		gh api "repos/ONLYOFFICE/onlyoffice.github.io/contents/$1" --jq '.[] | [.type,.path,.download_url] | @tsv' |
			while IFS="$(printf '\t')" read -r kind path url; do
				if [ "$kind" = dir ]; then
					fetch "$path" "$2"
				else
					out="$2/${path#sdkjs-plugins/content/languagetool/}"
					mkdir -p "$(dirname "$out")"
					curl -sSfL "$url" -o "$out"
				fi
			done
	}
	fetch sdkjs-plugins/content/languagetool "$2"
	cp "$2/scripts/langTool.js" "$2/scripts/langTool.js.stock"
	;;
A | B)
	sed -e "s#https://api.languagetoolplus.com/v2/#$SERVICE/api/v2/#" \
		"$2/scripts/langTool.js.stock" >"$2/scripts/langTool.js"
	if [ "$1" = B ]; then
		sed -i.bak \
			-e "s#^\([[:space:]]*\)method: 'GET',#&\n\1xhrFields: {withCredentials: true},#" \
			-e "s#^\([[:space:]]*\)method: 'POST',#&\n\1xhrFields: {withCredentials: true},#" \
			"$2/scripts/langTool.js"
		rm -f "$2/scripts/langTool.js.bak"
	fi
	diff -u "$2/scripts/langTool.js.stock" "$2/scripts/langTool.js" || true
	;;
*)
	echo "usage: plugin.sh fetch|A|B <dir>" >&2
	exit 2
	;;
esac
