#!/bin/sh
if [ -f /ssl/fullchain.pem ] && [ -f /ssl/privkey.pem ]; then
  cp /ssl/fullchain.pem /ssl/server-san.crt
  cp /ssl/privkey.pem /ssl/server-san.key
  chmod 644 /ssl/server-san.crt
  chmod 600 /ssl/server-san.key
  echo 'Certificates copied successfully'
else
  echo 'Let s Encrypt certificates not found'
  exit 1
fi