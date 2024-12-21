FROM alpine:3.21

WORKDIR /root/
COPY main .

CMD ["./main"]
