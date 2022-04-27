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
	

# $ cloc ./boxes/*.go ./ugcon/*.go *.go ../uggo/*.go ../ugform/*.go ../uggsec/*.go ../uggly-server/*.go ../uggly-server-login/*.go ../uggdyn/*.go
#       14 text files.
#       14 unique files.
#        0 files ignored.
# 
# github.com/AlDanial/cloc v 1.82  T=0.10 s (140.0 files/s, 57090.3 lines/s)
# -------------------------------------------------------------------------------
# Language                     files          blank        comment           code
# -------------------------------------------------------------------------------
# Go                              14            346            577           4785
# -------------------------------------------------------------------------------
# SUM:                            14            346            577           4785
# -------------------------------------------------------------------------------
