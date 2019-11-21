package spectrum

type Memory struct {
	data   [0x10000]byte
	speccy *Spectrum48k
}

func NewMemory() *Memory {
	return &Memory{}
}

func (memory *Memory) init(speccy *Spectrum48k) {
	memory.speccy = speccy
}

func (memory *Memory) reset() {
	for i := 0; i < 0x10000; i++ {
		memory.data[i] = 0
	}
}

func (memory *Memory) Read(address uint16) byte {
	return memory.data[address]
}

func (memory *Memory) Write(address uint16, value byte) {
	if (address >= SCREEN_BASE_ADDR) && (address < ATTR_BASE_ADDR) {
		memory.speccy.ula.screenBitmapWrite(address, memory.data[address], value)
	} else if (address >= ATTR_BASE_ADDR) && (address < 0x5b00) {
		memory.speccy.ula.screenAttrWrite(address, memory.data[address], value)
	}
	if address >= 0x4000 {
		memory.data[address] = value
	}
}

func (memory *Memory) Data() []byte {
	return memory.data[:]
}

func init() {
}
