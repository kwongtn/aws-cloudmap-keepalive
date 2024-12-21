# aws-cloudmap-keepalive

By default, this image does the following:

- Reads configmap
- Curls endpoints via configs in configmap to see if they are alive
- If not alive, delete them (will be recreated by other controllers)