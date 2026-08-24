package inspector

import (
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
)

func inspectBinary(file *os.File, header []byte, format string) (*BinaryInfo, error) {
	switch format {
	case "ELF":
		parsed, err := elf.NewFile(file)
		if err != nil {
			return nil, err
		}
		defer parsed.Close()
		bits := 0
		if parsed.Class == elf.ELFCLASS32 {
			bits = 32
		} else if parsed.Class == elf.ELFCLASS64 {
			bits = 64
		}
		endian := "little"
		if parsed.Data == elf.ELFDATA2MSB {
			endian = "big"
		}
		return &BinaryInfo{Format: "ELF", Architectures: []string{elfMachineName(uint16(parsed.Machine))}, Bits: bits, Endianness: endian}, nil
	case "Mach-O":
		if fat, err := macho.NewFatFile(file); err == nil {
			defer fat.Close()
			info := &BinaryInfo{Format: "Mach-O universal"}
			for _, arch := range fat.Arches {
				if len(info.Architectures) >= 16 {
					break
				}
				info.Architectures = append(info.Architectures, machoCPUName(arch.Cpu.String()))
			}
			return info, nil
		}
		parsed, err := macho.NewFile(file)
		if err != nil {
			return nil, err
		}
		defer parsed.Close()
		bits := 32
		if parsed.Magic == macho.Magic64 {
			bits = 64
		}
		endian := "little"
		if parsed.ByteOrder == binary.BigEndian {
			endian = "big"
		}
		return &BinaryInfo{Format: "Mach-O", Architectures: []string{machoCPUName(parsed.Cpu.String())}, Bits: bits, Endianness: endian}, nil
	case "PE":
		parsed, err := pe.NewFile(file)
		if err != nil {
			return nil, err
		}
		defer parsed.Close()
		bits := 0
		switch parsed.OptionalHeader.(type) {
		case *pe.OptionalHeader32:
			bits = 32
		case *pe.OptionalHeader64:
			bits = 64
		}
		return &BinaryInfo{Format: "PE", Architectures: []string{peMachineName(parsed.Machine)}, Bits: bits, Endianness: "little"}, nil
	case "Java class":
		if !isJavaClass(header) {
			return nil, fmt.Errorf("invalid Java class header")
		}
		return &BinaryInfo{Format: "Java class"}, nil
	default:
		if info := parseELFHeader(header); info != nil {
			return info, nil
		}
		return nil, fmt.Errorf("unsupported executable format %s", format)
	}
}

func machoCPUName(value string) string {
	value = strings.ToLower(value)
	switch {
	case strings.Contains(value, "arm64"):
		return "arm64"
	case strings.Contains(value, "amd64"):
		return "x86_64"
	case strings.Contains(value, "386"):
		return "x86"
	case strings.Contains(value, "arm"):
		return "arm"
	default:
		return bounded(value, 64)
	}
}

func peMachineName(machine uint16) string {
	switch machine {
	case pe.IMAGE_FILE_MACHINE_AMD64:
		return "x86_64"
	case pe.IMAGE_FILE_MACHINE_I386:
		return "x86"
	case pe.IMAGE_FILE_MACHINE_ARM:
		return "arm"
	case pe.IMAGE_FILE_MACHINE_ARM64:
		return "arm64"
	default:
		return fmt.Sprintf("machine-0x%x", machine)
	}
}
