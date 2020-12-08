#!/bin/bash
#
#  Copyright 2016 by Leipzig University Library, http://ub.uni-leipzig.de
#                    The Finc Authors, http://finc.info
#                    Martin Czygan, <martin.czygan@uni-leipzig.de>
#
# This file is part of some open source application.
#
# Some open source application is free software: you can redistribute
# it and/or modify it under the terms of the GNU General Public
# License as published by the Free Software Foundation, either
# version 3 of the License, or (at your option) any later version.
#
# Some open source application is distributed in the hope that it will
# be useful, but WITHOUT ANY WARRANTY; without even the implied warranty
# of MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
# GNU General Public License for more details.
#
# You should have received a copy of the GNU General Public License
# along with Foobar.  If not, see <http://www.gnu.org/licenses/>.
#
# @license GPL-3.0+ <http://spdx.org/licenses/GPL-3.0+>

_metha_endpoints()
{
    hash metha-ls 2>/dev/null || { return 1; }
    local cur=${COMP_WORDS[COMP_CWORD]}
    _get_comp_words_by_ref -n : cur
    COMPREPLY=( $(compgen -W "$(metha-ls|cut -f4)" -- $cur) )
    __ltrim_colon_completions "$cur"
}

complete -F _metha_endpoints metha-cat
complete -F _metha_endpoints metha-sync
complete -F _metha_endpoints metha-id
complete -F _metha_endpoints metha-files
