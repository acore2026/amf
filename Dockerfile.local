FROM alpine:3.15

LABEL description="Free5GC open source 5G Core Network - AMF" \
    version="Stage 3"

ENV F5GC_MODULE=amf

RUN apk add --no-cache tini

WORKDIR /free5gc
RUN mkdir -p config/ log/ cert/

COPY amf-binary ./amf
RUN chmod +x ./amf

COPY amf.pem ./cert/
COPY amf.key ./cert/

VOLUME [ "/free5gc/config" ]
VOLUME [ "/free5gc/cert" ]

EXPOSE 8000

ENTRYPOINT [ "/sbin/tini", "--" ]
CMD [ "./amf" ]