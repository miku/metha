# OJS list journals

[OJS](https://pkp.sfu.ca/ojs/) is an open source software application for
managing and publishing scholarly journals.

It seems to have OAI enabled by default. OJS allows to manage multiple journals
for a site, e.g.
[https://www.aaai.org/ocs/index.php](https://www.aaai.org/ocs/index.php).

Given such a supersite, extract all OAI endpoints quickly.

```
$ ./ojslist.sh -i https://www.aaai.org/ocs/index.php
https://www.aaai.org/ocs/index.php/AAAI/index/oai
https://www.aaai.org/ocs/index.php/AIIDE/index/oai
https://www.aaai.org/ocs/index.php/DC/index/oai
https://www.aaai.org/ocs/index.php/EAAI/index/oai
https://www.aaai.org/ocs/index.php/ECP/index/oai
https://www.aaai.org/ocs/index.php/FLAIRS/index/oai
https://www.aaai.org/ocs/index.php/FSS/index/oai
https://www.aaai.org/ocs/index.php/HCOMP/index/oai
https://www.aaai.org/ocs/index.php/IAAI/index/oai
https://www.aaai.org/ocs/index.php/ICAPS/index/oai
https://www.aaai.org/ocs/index.php/ICCCD/index/oai
https://www.aaai.org/ocs/index.php/ICWSM/index/oai
https://www.aaai.org/ocs/index.php/IJCAI/index/oai
https://www.aaai.org/ocs/index.php/INT/index/oai
https://www.aaai.org/ocs/index.php/KR/index/oai
https://www.aaai.org/ocs/index.php/SARA/index/oai
https://www.aaai.org/ocs/index.php/SOCS/index/oai
https://www.aaai.org/ocs/index.php/SSS/index/oai
https://www.aaai.org/ocs/index.php/WS/index/oai
```

More examples:

* http://www.uel.br/revistas/uel/index.php
* https://www.uni-hildesheim.de/ojs/index.php/
