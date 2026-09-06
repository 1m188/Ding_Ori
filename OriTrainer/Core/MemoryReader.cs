using System;
using System.Diagnostics;
using System.Runtime.InteropServices;

namespace OriTrainer.Core
{
    /// <summary>
    /// 32 位目标进程的内存读写、模块基址、指针链遍历与 AOB 特征码扫描。
    /// 本程序按 x86 编译、目标游戏同为 32 位，所有指针一律按 4 字节处理。
    /// </summary>
    internal sealed class MemoryReader : IDisposable
    {
        private Process _process;
        private IntPtr _handle;

        public int ProcessId { get; private set; }
        public string ProcessName { get; private set; }

        public bool Attached
        {
            get { return _handle != IntPtr.Zero && _process != null && !_process.HasExited; }
        }

        public bool Attach(string processNameWithoutExtension)
        {
            Detach();
            Process[] procs;
            try { procs = Process.GetProcessesByName(processNameWithoutExtension); }
            catch (Exception) { return false; }
            if (procs == null || procs.Length == 0) return false;

            _process = procs[0];
            ProcessId = _process.Id;
            ProcessName = _process.ProcessName;
            _handle = NativeMethods.OpenProcess(NativeMethods.PROCESS_ACCESS, false, ProcessId);
            if (_handle == IntPtr.Zero)
            {
                _process.Dispose();
                _process = null;
                return false;
            }
            return true;
        }

        public void Detach()
        {
            if (_handle != IntPtr.Zero) { NativeMethods.CloseHandle(_handle); _handle = IntPtr.Zero; }
            if (_process != null) { _process.Dispose(); _process = null; }
            ProcessId = 0;
            ProcessName = null;
        }

        public void Dispose() { Detach(); }

        /// <summary>地址 + 有符号偏移（32 位回绕）。</summary>
        public static UIntPtr Add(UIntPtr address, long offset)
        {
            unchecked { return new UIntPtr((uint)((long)address.ToUInt32() + offset)); }
        }

        // ---------- 基础读写 ----------

        public bool ReadBytes(UIntPtr address, byte[] buffer)
        {
            if (!Attached || address == UIntPtr.Zero || buffer.Length == 0) return false;
            IntPtr read;
            IntPtr native = (IntPtr)unchecked((int)address.ToUInt32());
            return NativeMethods.ReadProcessMemory(_handle, native, buffer,
                (IntPtr)buffer.Length, out read) && read == (IntPtr)buffer.Length;
        }

        public bool WriteBytes(UIntPtr address, byte[] data)
        {
            if (!Attached || address == UIntPtr.Zero || data.Length == 0) return false;
            IntPtr written;
            IntPtr native = (IntPtr)unchecked((int)address.ToUInt32());
            return NativeMethods.WriteProcessMemory(_handle, native, data,
                (IntPtr)data.Length, out written) && written == (IntPtr)data.Length;
        }

        public bool ReadU32(UIntPtr address, out uint value)
        {
            value = 0;
            var buf = new byte[4];
            if (!ReadBytes(address, buf)) return false;
            value = BitConverter.ToUInt32(buf, 0);
            return true;
        }

        public bool WriteU32(UIntPtr address, uint value)
        {
            return WriteBytes(address, BitConverter.GetBytes(value));
        }

        public bool ReadInt32(UIntPtr address, out int value)
        {
            uint u;
            if (!ReadU32(address, out u)) { value = 0; return false; }
            value = unchecked((int)u);
            return true;
        }

        public bool WriteInt32(UIntPtr address, int value)
        {
            return WriteU32(address, unchecked((uint)value));
        }

        public bool ReadFloat(UIntPtr address, out float value)
        {
            value = 0f;
            var buf = new byte[4];
            if (!ReadBytes(address, buf)) return false;
            value = BitConverter.ToSingle(buf, 0);
            return true;
        }

        public bool WriteFloat(UIntPtr address, float value)
        {
            return WriteBytes(address, BitConverter.GetBytes(value));
        }

        // ---------- 模块基址 ----------

        public bool TryGetModuleBase(string moduleName, out UIntPtr baseAddress)
        {
            baseAddress = UIntPtr.Zero;
            if (!Attached) return false;
            try
            {
                foreach (ProcessModule m in _process.Modules)
                {
                    if (string.Equals(m.ModuleName, moduleName, StringComparison.OrdinalIgnoreCase))
                    {
                        baseAddress = new UIntPtr(unchecked((uint)m.BaseAddress.ToInt32()));
                        return true;
                    }
                }
            }
            catch (Exception) { }
            return false;
        }

        // ---------- 指针链 ----------
        /// <summary>
        /// 指针链遍历：base + offsets[0] 解引用 → + offsets[1] 解引用 → …
        /// 最终地址 = 末级指针值 + offsets[N-1]（不再解引用）。
        /// 与 CE 表的对应关系见 Ori/OriOffsets.cs 的注释。
        /// </summary>
        public bool WalkChain(UIntPtr baseAddress, int[] offsets, out UIntPtr finalAddress)
        {
            finalAddress = UIntPtr.Zero;
            if (!Attached || offsets == null || offsets.Length == 0) return false;

            UIntPtr current = Add(baseAddress, offsets[0]);
            for (int i = 1; i < offsets.Length; i++)
            {
                uint ptr;
                if (!ReadU32(current, out ptr) || ptr == 0) return false;
                current = Add(new UIntPtr(ptr), offsets[i]);
            }
            finalAddress = current;
            return true;
        }

        // ---------- AOB 特征码扫描 ----------
        /// <summary>
        /// 在目标已提交的可执行内存中搜索特征码（-1 表示通配字节），返回首个匹配地址。
        /// 注意 Mono 的 JIT 代码只有对应方法被执行过才存在，因此“未找到”不代表失败，稍后重试即可。
        /// </summary>
        public bool FindPattern(int[] pattern, out UIntPtr matchAddress)
        {
            matchAddress = UIntPtr.Zero;
            if (!Attached || pattern == null || pattern.Length == 0) return false;

            int plen = pattern.Length;
            int firstIdx = 0;
            while (firstIdx < plen && pattern[firstIdx] < 0) firstIdx++;
            if (firstIdx >= plen) return false;
            byte first = (byte)pattern[firstIdx];

            long addr = 0x10000L;
            const long maxAddr = 0x7FFEFFFFL;
            int mbiSize = Marshal.SizeOf(typeof(NativeMethods.MEMORY_BASIC_INFORMATION));

            while (addr < maxAddr)
            {
                NativeMethods.MEMORY_BASIC_INFORMATION mbi;
                if (!NativeMethods.VirtualQueryEx(_handle, (IntPtr)addr, out mbi, (IntPtr)mbiSize)) break;
                long regionSize = mbi.RegionSize.ToInt64();
                if (regionSize <= 0) break;

                long regionBase = mbi.BaseAddress.ToInt64();
                bool committed = mbi.State == NativeMethods.MEM_COMMIT;
                bool executable = (mbi.Protect & 0xF0) != 0
                                  && (mbi.Protect & NativeMethods.PAGE_GUARD) == 0
                                  && mbi.Protect != NativeMethods.PAGE_NOACCESS;

                if (committed && executable && regionSize >= plen && regionSize <= (32L << 20))
                {
                    var buf = new byte[regionSize];
                    IntPtr read;
                    if (NativeMethods.ReadProcessMemory(_handle, (IntPtr)regionBase, buf,
                        (IntPtr)buf.Length, out read) && read == (IntPtr)buf.Length)
                    {
                        long limit = buf.Length - plen;
                        for (long i = 0; i <= limit; i++)
                        {
                            if (buf[i + firstIdx] != first) continue;
                            bool ok = true;
                            for (int j = 0; j < plen; j++)
                            {
                                int p = pattern[j];
                                if (p >= 0 && buf[i + j] != (byte)p) { ok = false; break; }
                            }
                            if (ok)
                            {
                                matchAddress = new UIntPtr(unchecked((uint)(regionBase + i)));
                                return true;
                            }
                        }
                    }
                }
                addr = regionBase + regionSize;
            }
            return false;
        }
    }
}
