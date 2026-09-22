using System;
using System.Runtime.InteropServices;

namespace OriTrainerDEDLL
{
    // 管道必须用 Win32 API：Mono 2.x 的托管 NamedPipeServerStream 会把客户端发来的
    // 字节原样回显给客户端（实测），无法用来收命令。
    internal static class Native
    {
        public const uint PIPE_ACCESS_DUPLEX = 0x00000003;
        public const uint PIPE_TYPE_BYTE = 0x00000000;
        public const uint PIPE_READMODE_BYTE = 0x00000000;
        public const uint PIPE_WAIT = 0x00000000;
        public const uint PIPE_UNLIMITED_INSTANCES = 255;

        [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
        public static extern IntPtr CreateNamedPipeW(string name, uint openMode, uint pipeMode,
            uint maxInstances, uint outBufferSize, uint inBufferSize, uint timeout, IntPtr security);

        [DllImport("kernel32.dll", SetLastError = true)]
        public static extern bool ConnectNamedPipe(IntPtr pipe, IntPtr overlapped);

        [DllImport("kernel32.dll", SetLastError = true)]
        public static extern bool DisconnectNamedPipe(IntPtr pipe);

        [DllImport("kernel32.dll", SetLastError = true)]
        public static extern bool ReadFile(IntPtr handle, [Out] byte[] buffer,
            uint toRead, out uint read, IntPtr overlapped);
    }
}
