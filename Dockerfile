FROM golang:1.20.2-bullseye AS build

WORKDIR /go/src/gptask

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .
RUN go build -v -o /usr/local/bin/gptask .

####################

FROM ubuntu:22.04

RUN apt update \
  && apt install -y --no-install-recommends tzdata curl jq zip python3 python3-pip awscli \
  && echo 'APT::Get::Assume-Yes "true";' > /etc/apt/apt.conf.d/90yes \
  && mkdir /opt/gptask

RUN pip3 install requests boto3

COPY --from=build /usr/local/bin/gptask /usr/local/bin/gptask

CMD ["/usr/local/bin/gptask"]

