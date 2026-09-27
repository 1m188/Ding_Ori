using System;
using System.Collections.Generic;

namespace SharpMonoInjector
{
    public class Assembler
    {
        private readonly List<byte> _asm = new List<byte>();

        public void MovRax(IntPtr arg)
        {
            _asm.AddRange(new byte[] {0x48, 0xB8});
            _asm.AddRange(BitConverter.GetBytes((long)arg));
        }

        public void MovRcx(IntPtr arg)
        {
            _asm.AddRange(new byte[] {0x48, 0xB9});
            _asm.AddRange(BitConverter.GetBytes((long)arg));
        }

        public void MovRdx(IntPtr arg)
        {
            _asm.AddRange(new byte[] {0x48, 0xBA});
            _asm.AddRange(BitConverter.GetBytes((long)arg));
        }

        public void MovR8(IntPtr arg)
        {
            _asm.AddRange(new byte[] {0x49, 0xB8});
            _asm.AddRange(BitConverter.GetBytes((long)arg));
        }

        public void MovR9(IntPtr arg)
        {
            _asm.AddRange(new byte[] {0x49, 0xB9});
            _asm.AddRange(BitConverter.GetBytes((long)arg));
        }

        public void SubRsp(byte arg)
        {
            _asm.AddRange(new byte[] {0x48, 0x83, 0xEC});
            _asm.Add(arg);
        }

        public void CallRax()
        {
            _asm.AddRange(new byte[] {0xFF, 0xD0});
        }

        public void AddRsp(byte arg)
        {
            _asm.AddRange(new byte[] {0x48, 0x83, 0xC4});
            _asm.Add(arg);
        }

        public void MovRaxTo(IntPtr dest)
        {
            _asm.AddRange(new byte[] { 0x48, 0xA3 });
            _asm.AddRange(BitConverter.GetBytes((long)dest));
        }

        public void Push(IntPtr arg)
        {
            // 0x6A = push imm8，只接受有符号字节 [-128, 127]；
            // 0x68 = push imm32。上游用 (<128) 选 opcode、用 (<=255) 选长度，
            // 两个界不一致，128..255 会写成 imm32 操作码 + 1 字节操作数，指令错位。
            if ((int)arg >= -128 && (int)arg < 128) {
                _asm.Add(0x6A);
                _asm.Add((byte)(int)arg);
            } else {
                _asm.Add(0x68);
                _asm.AddRange(BitConverter.GetBytes((int)arg));
            }
        }

        public void MovEax(IntPtr arg)
        {
            _asm.Add(0xB8);
            _asm.AddRange(BitConverter.GetBytes((int)arg));
        }

        public void CallEax()
        {
            _asm.AddRange(new byte[] {0xFF, 0xD0});
        }

        public void AddEsp(byte arg)
        {
            _asm.AddRange(new byte[] {0x83, 0xC4});
            _asm.Add(arg);
        }

        public void MovEaxTo(IntPtr dest)
        {
            _asm.Add(0xA3);
            _asm.AddRange(BitConverter.GetBytes((int)dest));
        }

        public void Return()
        {
            _asm.Add(0xC3);
        }

        public byte[] ToByteArray() => _asm.ToArray();
    }
}
