FROM golang:1.20.2-bullseye AS build

WORKDIR /go/src/gptask

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .
RUN go build -v -o /usr/local/bin/gptask .

####################

FROM ubuntu:22.04

RUN apt update \
  && apt install -y --no-install-recommends \
    software-properties-common \
    sudo \
    tzdata \
    psmisc \
    curl \
    gnupg \
    jq \
    zip \
    awscli \
  && echo 'APT::Get::Assume-Yes "true";' >> /etc/apt/apt.conf.d/90gptask \
  && echo 'quiet "2";' >> /etc/apt/apt.conf.d/90gptask \
  && mkdir /opt/gptask

RUN add-apt-repository ppa:longsleep/golang-backports \
  && apt update \
  && apt install -y golang-go

RUN curl -fsSL https://deb.nodesource.com/setup_lts.x | sudo -E bash - \
   && apt install -y nodejs

RUN apt install python3 python3-pip \
  && pip3 install requests boto3

COPY --from=build /usr/local/bin/gptask /usr/local/bin/gptask

CMD ["/usr/local/bin/gptask"]

