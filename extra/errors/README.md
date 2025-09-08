# misc oai errors

```
INFO[0022] wrote /home/tir/.cache/metha/I29haV9kYyNodHRwczovL2Rhcndpbi5uYXR1cmFsc2NpZW5jZXMuYmUvcG9ydGFsL2FwcF9kZXYucGhwL29haXBtaC8/2024-02-29-00000000.xml-tmp-1489848
INFO[0022] moved 1 file(s) into place
INFO[0022] https://darwin.naturalsciences.be/portal/app_dev.php/oaipmh/?from=2024-03-01&metadataPrefix=oai_dc&until=2024-03-31&verb=ListRecords
FATA[0022] oai: unknownError
{"error":{"root_cause":[{"type":"exception","reason":"Trying to create too many
scroll contexts. Must be less than or equal to: [500]. This limit can be set by
changing the [search.max_open_scroll_context]
setting."}],"type":"search_phase_execution_exception","reason":"all shards
failed","phase":"query","grouped":true,"failed_shards":[{"shard":0,"index":"naturalheritage","node":"9e4DV69MR7GyaMI2wrIBdg","reason":{"type":"exception","reason":"Trying
to create too many scroll contexts. Must be less than or equal to: [500]. This
limit can be set by changing the [search.max_open_scroll_context]
setting."}}]},"status":500}
/var/www/html/portal_nh_elastic/vendor/elasticsearch/elasticsearch/src/Elasticsearch/Connections/Connection.php
674
```

* https://darwin.naturalsciences.be/portal/app_dev.php/_profiler/open?file=web/app_dev.php&line=29#line29
