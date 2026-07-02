package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"

	nasMessage "github.com/acore2026/nas/nasMessage"
)

func main() {
	hexData := "7e00e10271006e0100006a05000000057b22696e74656e744964223a2264313261396135372d373538312d343635342d383439392d363461363662346464613963222c22696e74656e744465736372697074696f6e223a226869222c226f626a656374223a224e65774e415341646170746572227d"
	
	fmt.Println("=== ULCooperation NAS Message Decode Test ===")
	fmt.Printf("原始十六进制码流:\n%s\n\n", hexData)
	
	data, err := hex.DecodeString(hexData)
	if err != nil {
		fmt.Printf("十六进制解码失败: %v\n", err)
		return
	}
	
	fmt.Printf("解码后字节数组 (长度: %d):\n", len(data))
	for i, b := range data {
		if i%16 == 0 {
			fmt.Printf("\n%04d: ", i)
		}
		fmt.Printf("%02x ", b)
	}
	fmt.Println("\n")
	
	fmt.Println("=== 手动解析 NAS 头部 ===")
	buffer := bytes.NewBuffer(data)
	
	var epd uint8
	var secHeader uint8
	var messageType uint8
	var messageIdentity uint8
	
	if err := binary.Read(buffer, binary.BigEndian, &epd); err != nil {
		fmt.Printf("读取 EPD 失败: %v\n", err)
		return
	}
	fmt.Printf("ExtendedProtocolDiscriminator: 0x%02x (%d)\n", epd, epd)
	
	if err := binary.Read(buffer, binary.BigEndian, &secHeader); err != nil {
		fmt.Printf("读取安全头类型失败: %v\n", err)
		return
	}
	fmt.Printf("SpareHalfOctetAndSecurityHeaderType: 0x%02x (%d)\n", secHeader, secHeader)
	
	if err := binary.Read(buffer, binary.BigEndian, &messageType); err != nil {
		fmt.Printf("读取 MessageType 失败: %v\n", err)
		return
	}
	fmt.Printf("MessageType: 0x%02x (%d)\n", messageType, messageType)
	
	if err := binary.Read(buffer, binary.BigEndian, &messageIdentity); err != nil {
		fmt.Printf("读取 MessageIdentity 失败: %v\n", err)
		return
	}
	fmt.Printf("MessageIdentity: 0x%02x (%d)\n\n", messageIdentity, messageIdentity)
	
	fmt.Println("=== 解析信息元素 (IE) ===")
	var iei uint8
	var ieLen uint16
	
	if err := binary.Read(buffer, binary.BigEndian, &iei); err != nil {
		fmt.Printf("读取 IEI 失败: %v\n", err)
		return
	}
	fmt.Printf("IEI: 0x%02x (%d)\n", iei, iei)
	
	if err := binary.Read(buffer, binary.BigEndian, &ieLen); err != nil {
		fmt.Printf("读取 IE Length 失败: %v\n", err)
		return
	}
	fmt.Printf("IE Length: %d bytes (0x%04x)\n", ieLen, ieLen)
	
	ieContents := make([]byte, ieLen)
	if err := binary.Read(buffer, binary.BigEndian, ieContents); err != nil {
		fmt.Printf("读取 IE 内容失败: %v\n", err)
		return
	}
	
	fmt.Printf("\nIE 内容 (%d bytes):\n", len(ieContents))
	for i, b := range ieContents {
		if i%16 == 0 {
			fmt.Printf("%04d: ", i)
		}
		fmt.Printf("%02x ", b)
		if (i+1)%16 == 0 {
			fmt.Println()
		}
	}
	fmt.Println("\n")
	
	if len(ieContents) >= 9 {
		fmt.Println("=== 解析 Container 字段 ===")
		containerBuffer := bytes.NewBuffer(ieContents)
		
		var containerType uint16
		var containerContentLength uint16
		var containerTypePTI uint8
		var containerContent uint32
		
		binary.Read(containerBuffer, binary.BigEndian, &containerType)
		binary.Read(containerBuffer, binary.BigEndian, &containerContentLength)
		binary.Read(containerBuffer, binary.BigEndian, &containerTypePTI)
		binary.Read(containerBuffer, binary.BigEndian, &containerContent)
		
		fmt.Printf("ContainerType: 0x%04x (%d)\n", containerType, containerType)
		fmt.Printf("ContainerContentLength: %d (0x%04x)\n", containerContentLength, containerContentLength)
		fmt.Printf("ContainerTypePTI: 0x%02x (%d)\n", containerTypePTI, containerTypePTI)
		fmt.Printf("ContainerContent: 0x%08x (%d)\n\n", containerContent, containerContent)
		
		remaining := containerBuffer.Bytes()
		fmt.Printf("剩余内容 (%d bytes):\n", len(remaining))
		for i, b := range remaining {
			if i%16 == 0 {
				fmt.Printf("%04d: ", i)
			}
			fmt.Printf("%02x ", b)
			if (i+1)%16 == 0 {
				fmt.Println()
			}
		}
		fmt.Println()
		
		if len(remaining) > 0 {
			fmt.Printf("\n尝试解析为文本:\n%s\n", string(remaining))
			
			var jsonContent interface{}
			if err := json.Unmarshal(remaining, &jsonContent); err == nil {
				prettyJSON, _ := json.MarshalIndent(jsonContent, "", "  ")
				fmt.Printf("\n格式化的 JSON:\n%s\n", prettyJSON)
			}
		}
	}
	
	fmt.Println("\n=== 使用 NAS 库解码 ===")
	ulCooperation := nasMessage.NewULCooperation(messageType)
	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)
	
	if err := ulCooperation.DecodeULCooperationV2(&dataCopy); err != nil {
		fmt.Printf("NAS 库解码失败: %v\n", err)
		return
	}
	
	fmt.Printf("\n解码成功!\n")
	fmt.Printf("MessageType: 0x%02x\n", ulCooperation.MessageType)
	fmt.Printf("MessageIdentity: 0x%02x\n", ulCooperation.MessageIdentity)
	
	if ulCooperation.ULApContainer != nil {
		ie := ulCooperation.ULApContainer
		fmt.Printf("\nULApContainer IE:\n")
		fmt.Printf("  IEI: 0x%02x (%d)\n", ie.GetIei(), ie.GetIei())
		fmt.Printf("  Len: %d bytes (0x%04x)\n", ie.GetLen(), ie.GetLen())
		fmt.Printf("  ContainerType: 0x%04x (%d)\n", ie.GetContainerType(), ie.GetContainerType())
		fmt.Printf("  ContainerContentLength: %d (0x%04x)\n", ie.GetContainerContentLength(), ie.GetContainerContentLength())
		fmt.Printf("  ContainerTypePTI: 0x%02x (%d)\n", ie.GetContainerTypePTI(), ie.GetContainerTypePTI())
		fmt.Printf("  ContainerContent: 0x%08x (%d)\n", ie.GetContainerContent(), ie.GetContainerContent())
		fmt.Printf("  Contents length: %d bytes\n", len(ie.Contents))
		
		if len(ie.Contents) > 9 {
			remaining := ie.Contents[9:]
			fmt.Printf("\n  Payload (after container fields):\n")
			fmt.Printf("  Length: %d bytes\n", len(remaining))
			fmt.Printf("  Hex: %x\n", remaining)
			fmt.Printf("  Text: %s\n", string(remaining))
			
			var jsonContent interface{}
			if err := json.Unmarshal(remaining, &jsonContent); err == nil {
				prettyJSON, _ := json.MarshalIndent(jsonContent, "  ", "  ")
				fmt.Printf("\n  JSON Payload:\n  %s\n", prettyJSON)
			}
		}
	} else {
		fmt.Printf("\n警告: ULApContainer 为空!\n")
	}
	
	if len(ulCooperation.UnknownIEs) > 0 {
		fmt.Printf("\nUnknown IEs: %d\n", len(ulCooperation.UnknownIEs))
		for i, ie := range ulCooperation.UnknownIEs {
			fmt.Printf("  IE[%d]: IEI=0x%02x, Len=%d\n", i, ie.GetIei(), ie.GetLen())
		}
	}
}