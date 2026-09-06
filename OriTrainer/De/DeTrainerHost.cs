using System;
using System.Diagnostics;
using System.IO;
using System.Threading;
using System.Windows.Forms;
using OriTrainer.Core;

namespace OriTrainer.De
{
    /// <summary>
    /// 终极版（FLiNG）修改器宿主：
    /// 1) 从内嵌资源释放到 %TEMP%\OriTrainer\（必须保留原始文件名——该 exe 内部引用自身文件名）；
    /// 2) 启动子进程，等待主窗口；
    /// 3) SetParent 嵌入指定容器（去标题栏改子窗口样式），失败可降级为独立窗口；
    /// 4) Cleanup 结束子进程并删除临时文件。
    /// </summary>
    internal sealed class DeTrainerHost
    {
        private const string ResourceName = "OriTrainer.fling_de_trainer.exe";
        private const string ExeFileName = "Ori and the Blind Forest Definitive Edition v1.0 Plus 13 Trainer.exe";

        private Process _child;
        private IntPtr _childHwnd;
        private string _exePath;

        public bool IsRunning
        {
            get { return _child != null && !_child.HasExited; }
        }

        public string Extract()
        {
            var dir = Path.Combine(Path.GetTempPath(), "OriTrainer");
            Directory.CreateDirectory(dir);
            _exePath = Path.Combine(dir, ExeFileName);

            byte[] data = ReadEmbedded();
            var fi = new FileInfo(_exePath);
            if (!fi.Exists || fi.Length != data.Length)
                File.WriteAllBytes(_exePath, data);
            return _exePath;
        }

        private static byte[] ReadEmbedded()
        {
            var asm = typeof(DeTrainerHost).Assembly;
            using (var s = asm.GetManifestResourceStream(ResourceName))
            {
                if (s == null)
                    throw new InvalidOperationException("未找到内嵌资源 " + ResourceName);
                using (var ms = new MemoryStream())
                {
                    s.CopyTo(ms);
                    return ms.ToArray();
                }
            }
        }

        public bool Launch()
        {
            var psi = new ProcessStartInfo(_exePath)
            {
                WorkingDirectory = Path.GetDirectoryName(_exePath)
            };
            _child = Process.Start(psi);
            return _child != null;
        }

        /// <summary>等待子进程主窗口并尝试嵌入容器；失败返回 false（子进程继续以独立窗口运行）。</summary>
        public bool TryEmbed(Control container)
        {
            _childHwnd = IntPtr.Zero;
            for (int i = 0; i < 60; i++)
            {
                if (!IsRunning) return false;
                IntPtr h;
                try
                {
                    _child.Refresh();
                    h = _child.MainWindowHandle;
                }
                catch (Exception) { return false; }

                if (h != IntPtr.Zero && NativeMethods.IsWindow(h) && NativeMethods.IsWindowVisible(h))
                {
                    _childHwnd = h;
                    break;
                }
                Thread.Sleep(250);
            }
            if (_childHwnd == IntPtr.Zero || container == null || !container.IsHandleCreated)
                return false;

            try
            {
                int style = NativeMethods.GetWindowLong(_childHwnd, NativeMethods.GWL_STYLE);
                style &= ~(NativeMethods.WS_CAPTION | NativeMethods.WS_THICKFRAME | NativeMethods.WS_SYSMENU
                           | NativeMethods.WS_MINIMIZEBOX | NativeMethods.WS_MAXIMIZEBOX | NativeMethods.WS_POPUP);
                style |= NativeMethods.WS_CHILD | NativeMethods.WS_VISIBLE;
                NativeMethods.SetWindowLong(_childHwnd, NativeMethods.GWL_STYLE, style);
                NativeMethods.SetParent(_childHwnd, container.Handle);
                ResizeTo(container.ClientRectangle);
                NativeMethods.ShowWindow(_childHwnd, NativeMethods.SW_SHOW);
                return true;
            }
            catch (Exception)
            {
                return false;
            }
        }

        public void ResizeTo(System.Drawing.Rectangle r)
        {
            if (_childHwnd != IntPtr.Zero && NativeMethods.IsWindow(_childHwnd))
                NativeMethods.MoveWindow(_childHwnd, r.X, r.Y, r.Width, r.Height, true);
        }

        public void Cleanup()
        {
            try
            {
                if (IsRunning)
                {
                    if (_childHwnd != IntPtr.Zero)
                        NativeMethods.PostMessage(_childHwnd, NativeMethods.WM_CLOSE, IntPtr.Zero, IntPtr.Zero);
                    if (!_child.WaitForExit(3000))
                        _child.Kill();
                }
            }
            catch (Exception) { }
            finally
            {
                if (_child != null)
                {
                    try { _child.Dispose(); } catch (Exception) { }
                    _child = null;
                }
                _childHwnd = IntPtr.Zero;
                try
                {
                    if (_exePath != null && File.Exists(_exePath))
                        File.Delete(_exePath);
                }
                catch (Exception) { }
            }
        }
    }
}
