# OBS-based Replays for owlcms

This application records replay videos automatically, using the clock and decision information sent by owlcms.  The replays are trimmed to remove idle time before the actual lift.

This application is a cousin to the simpler https://github.com/owlcms/replays application

- The OBS Replay Source plugin is used to record the replays.  As a consequence, this application can only be used on platforms where this plugin is available.
- The files captured by the plugin are trimmed based on the clock information captured by listening to owlcms MQTT messages
- These trimmed files can be used as media source for streaming replays automatically.
- In addition, the trimmed files are made available to the jury using a web page.
