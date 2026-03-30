#!/bin/bash

for i in $(seq 1 6010); do
	curl -s https://v2.sherpa.ac.uk/id/repository/$i |
		pup 'div.summary_page_box_content > div.row json{}' |
		jq -r '.[] | select(.children[0].text == "OAI-PMH URL") | .children[1].children[0].href'
done 2>/dev/null | grep "^http"
