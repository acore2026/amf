package main

import (
	"bytes"
	"encoding/hex"
	"fmt"

	nasMessage "github.com/acore2026/nas/nasMessage"
)

func main() {
	fmt.Println("=== DLCooperation NAS Message Encode/Decode Test ===")
	
	ulHexData := "7e00e10271006e0100006a05000000057b22696e74656e744964223a2264313261396135372d373538312d343635342d383439392d363461363662346464613963222c22696e74656e744465736372697074696f6e223a226869222c226f626a656374223a224e65774e415341646170746572227d"
	
	ulData, err := hex.DecodeString(ulHexData)
	if err != nil {
		fmt.Printf("UL hex decode failed: %v\n", err)
		return
	}
	
	fmt.Println("\n=== Step 1: Decode ULCooperation ===")
	ulCooperation := nasMessage.NewULCooperation(0xe1)
	ulDataCopy := make([]byte, len(ulData))
	copy(ulDataCopy, ulData)
	
	if err := ulCooperation.DecodeULCooperationV2(&ulDataCopy); err != nil {
		fmt.Printf("UL decode failed: %v\n", err)
		return
	}
	
	fmt.Printf("ULCooperation decoded successfully:\n")
	fmt.Printf("  MessageType: 0x%02x\n", ulCooperation.MessageType)
	fmt.Printf("  MessageIdentity: 0x%02x\n", ulCooperation.MessageIdentity)
	
	if ulCooperation.ULApContainer != nil {
		ulIe := ulCooperation.ULApContainer
		fmt.Printf("  ULApContainer:\n")
		fmt.Printf("    IEI: 0x%02x\n", ulIe.GetIei())
		fmt.Printf("    Len: %d\n", ulIe.GetLen())
		fmt.Printf("    ContainerType: 0x%04x\n", ulIe.GetContainerType())
		fmt.Printf("    ContainerContentLength: %d\n", ulIe.GetContainerContentLength())
		fmt.Printf("    ContainerTypePTI: 0x%02x\n", ulIe.GetContainerTypePTI())
		fmt.Printf("    ContainerContent: 0x%08x\n", ulIe.GetContainerContent())
		
		if len(ulIe.Contents) > 9 {
			payload := ulIe.Contents[9:]
			fmt.Printf("    Payload: %s\n", string(payload))
		}
	}
	
	fmt.Println("\n=== Step 2: Create DLCooperation Response ===")
	dlCooperation := nasMessage.NewDLCooperation(0xe2)
	dlCooperation.SetExtendedProtocolDiscriminator(nasMessage.Epd5GSMobilityManagementMessage)
	dlCooperation.SpareHalfOctetAndSecurityHeaderType.SetSecurityHeaderType(0)
	dlCooperation.SpareHalfOctetAndSecurityHeaderType.SetSpareHalfOctet(0)
	
	if ulCooperation.ULApContainer != nil {
		dlApContainer := nasMessage.NewDLCooperationIE(nasMessage.DLCooperationDLApContainerType)
		dlApContainer.SetLen(ulCooperation.ULApContainer.GetLen())
		dlApContainer.SetContainerType(ulCooperation.ULApContainer.GetContainerType())
		dlApContainer.SetContainerContentLength(ulCooperation.ULApContainer.GetContainerContentLength())
		dlApContainer.SetContainerTypePTI(ulCooperation.ULApContainer.GetContainerTypePTI())
		dlApContainer.SetContainerContent(ulCooperation.ULApContainer.GetContainerContent())
		dlApContainer.SetContents(ulCooperation.ULApContainer.GetContents())
		
		dlCooperation.DLApContainer = dlApContainer
		
		fmt.Printf("DLCooperation created:\n")
		fmt.Printf("  MessageType: 0x%02x\n", dlCooperation.MessageType)
		fmt.Printf("  MessageIdentity: 0x%02x\n", dlCooperation.MessageIdentity)
		fmt.Printf("  DLApContainer:\n")
		fmt.Printf("    IEI: 0x%02x\n", dlApContainer.GetIei())
		fmt.Printf("    Len: %d\n", dlApContainer.GetLen())
		fmt.Printf("    ContainerType: 0x%04x\n", dlApContainer.GetContainerType())
		fmt.Printf("    ContainerContentLength: %d\n", dlApContainer.GetContainerContentLength())
		fmt.Printf("    ContainerTypePTI: 0x%02x\n", dlApContainer.GetContainerTypePTI())
		fmt.Printf("    ContainerContent: 0x%08x\n", dlApContainer.GetContainerContent())
	}
	
	fmt.Println("\n=== Step 3: Encode DLCooperation ===")
	buffer := new(bytes.Buffer)
	if err := dlCooperation.EncodeDLCooperation(buffer); err != nil {
		fmt.Printf("DL encode failed: %v\n", err)
		return
	}
	
	dlData := buffer.Bytes()
	fmt.Printf("DLCooperation encoded successfully (len=%d)\n", len(dlData))
	fmt.Printf("Hex: %s\n", hex.EncodeToString(dlData))
	
	fmt.Println("\nByte-by-byte:")
	for i, b := range dlData {
		if i%16 == 0 {
			fmt.Printf("%04d: ", i)
		}
		fmt.Printf("%02x ", b)
		if (i+1)%16 == 0 {
			fmt.Println()
		}
	}
	if len(dlData)%16 != 0 {
		fmt.Println()
	}
	
	fmt.Println("\n=== Step 4: Decode DLCooperation (verify) ===")
	dlCooperation2 := nasMessage.NewDLCooperation(0xe2)
	dlDataCopy := make([]byte, len(dlData))
	copy(dlDataCopy, dlData)
	
	if err := dlCooperation2.DecodeDLCooperation(&dlDataCopy); err != nil {
		fmt.Printf("DL decode failed: %v\n", err)
		return
	}
	
	fmt.Printf("DLCooperation decoded successfully:\n")
	fmt.Printf("  MessageType: 0x%02x\n", dlCooperation2.MessageType)
	fmt.Printf("  MessageIdentity: 0x%02x\n", dlCooperation2.MessageIdentity)
	
	if dlCooperation2.DLApContainer != nil {
		dlIe := dlCooperation2.DLApContainer
		fmt.Printf("  DLApContainer:\n")
		fmt.Printf("    IEI: 0x%02x\n", dlIe.GetIei())
		fmt.Printf("    Len: %d\n", dlIe.GetLen())
		fmt.Printf("    ContainerType: 0x%04x\n", dlIe.GetContainerType())
		fmt.Printf("    ContainerContentLength: %d\n", dlIe.GetContainerContentLength())
		fmt.Printf("    ContainerTypePTI: 0x%02x\n", dlIe.GetContainerTypePTI())
		fmt.Printf("    ContainerContent: 0x%08x\n", dlIe.GetContainerContent())
		
		if len(dlIe.Contents) > 9 {
			payload := dlIe.Contents[9:]
			fmt.Printf("    Payload: %s\n", string(payload))
		}
	}
	
	fmt.Println("\n=== Comparison ===")
	if ulCooperation.ULApContainer != nil && dlCooperation2.DLApContainer != nil {
		ulIe := ulCooperation.ULApContainer
		dlIe := dlCooperation2.DLApContainer
		
		match := true
		if ulIe.GetLen() != dlIe.GetLen() {
			fmt.Printf("❌ Len mismatch: UL=%d, DL=%d\n", ulIe.GetLen(), dlIe.GetLen())
			match = false
		}
		if ulIe.GetContainerType() != dlIe.GetContainerType() {
			fmt.Printf("❌ ContainerType mismatch: UL=0x%04x, DL=0x%04x\n", ulIe.GetContainerType(), dlIe.GetContainerType())
			match = false
		}
		if ulIe.GetContainerContentLength() != dlIe.GetContainerContentLength() {
			fmt.Printf("❌ ContainerContentLength mismatch: UL=%d, DL=%d\n", ulIe.GetContainerContentLength(), dlIe.GetContainerContentLength())
			match = false
		}
		if ulIe.GetContainerTypePTI() != dlIe.GetContainerTypePTI() {
			fmt.Printf("❌ ContainerTypePTI mismatch: UL=0x%02x, DL=0x%02x\n", ulIe.GetContainerTypePTI(), dlIe.GetContainerTypePTI())
			match = false
		}
		if ulIe.GetContainerContent() != dlIe.GetContainerContent() {
			fmt.Printf("❌ ContainerContent mismatch: UL=0x%08x, DL=0x%08x\n", ulIe.GetContainerContent(), dlIe.GetContainerContent())
			match = false
		}
		
		ulPayload := ulIe.Contents[9:]
		dlPayload := dlIe.Contents[9:]
		if string(ulPayload) != string(dlPayload) {
			fmt.Printf("❌ Payload mismatch:\n")
			fmt.Printf("  UL: %s\n", string(ulPayload))
			fmt.Printf("  DL: %s\n", string(dlPayload))
			match = false
		}
		
		if match {
			fmt.Println("✓ All fields match! DLCooperation successfully mirrors ULCooperation content")
		}
	}
}