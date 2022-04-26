#!/bin/bash

# $ cat ~/.grc/grc.conf
# regexp=clearing
# colour=green
# -
# regexp=setting
# colour=red
# -
# regexp=stream
# colour=blue


grc -c grc.conf tail -f uggcli.log.json | jq -c \
	'del(
		select(
			(select(.tags != null) 
				| .tags[] | contains("boxes"), contains("transform")),
			.msg == "got fg color",
			.msg == "lookup color"
		)
	) |
	select(. != null) | {time: .t, msg: .msg, o: del(.t,.msg)}'
	


