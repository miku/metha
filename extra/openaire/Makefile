SHELL := /bin/bash

openaire.csv:
	# https://explore.openaire.eu/search/content-providers
	curl -sL "https://services.openaire.eu/search/v2/api/reports?format=csv&type=datasources" | sed -e "s/\r/\n/g" > $@
