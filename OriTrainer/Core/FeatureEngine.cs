using System;
using System.Threading;

namespace OriTrainer.Core
{
    /// <summary>
    /// 可激活的修改功能。激活后由引擎线程周期调用 Tick 持续生效，
    /// 关闭时通过 OnDeactivated 还原（AOB 补丁还原字节、冻结功能清状态）。
    /// </summary>
    internal abstract class CheatFeature
    {
        public string Name = "";
        public int HotkeyNumber = -1;   // 1..9 / 0，-1 表示未绑定热键
        public bool Verified = true;    // false = 来自社区表、尚未实机验证
        public bool Active;
        public string Status = "未激活";

        /// <summary>引擎线程周期调用。返回本次是否成功。</summary>
        public abstract bool Tick(MemoryReader mem);

        public virtual void OnDeactivated(MemoryReader mem) { }

        /// <summary>目标进程更换（游戏重启）时调用，清空运行期缓存但不还原旧进程。</summary>
        public virtual void ResetForNewProcess() { }
    }

    /// <summary>
    /// AOB 特征码代码补丁：激活时扫描目标内存并把匹配处的指令替换为
    /// 预置字节（通常是 NOP），关闭时还原原始字节。对应 CE 表里的
    /// Auto Assembler aobscan 脚本。
    /// </summary>
    internal sealed class AobPatchFeature : CheatFeature
    {
        public sealed class PatchSpec
        {
            public string Description = "";
            public int[] Pattern = new int[0];   // -1 = 通配
            public int PatchOffset;
            public byte[] Replacement = new byte[0];
        }

        private sealed class PatchState
        {
            public PatchSpec Spec;
            public bool Applied;
            public UIntPtr Address;
            public byte[] Original;
        }

        private readonly PatchState[] _patches;
        private DateTime _lastScanUtc = DateTime.MinValue;
        private static readonly TimeSpan ScanInterval = TimeSpan.FromMilliseconds(2500);

        public AobPatchFeature(string name, int hotkey, params PatchSpec[] specs)
        {
            Name = name;
            HotkeyNumber = hotkey;
            Verified = false; // 社区表转写，需实机验证
            _patches = Array.ConvertAll(specs, s => new PatchState { Spec = s });
        }

        public override bool Tick(MemoryReader mem)
        {
            int pending = 0;
            foreach (var p in _patches) if (!p.Applied) pending++;
            if (pending == 0) { Status = "已补丁 ✓"; return true; }

            if (DateTime.UtcNow - _lastScanUtc < ScanInterval)
            {
                Status = "特征扫描中…（JIT 代码未生成，稍后重试）";
                return false;
            }
            _lastScanUtc = DateTime.UtcNow;

            foreach (var p in _patches)
            {
                if (p.Applied) continue;
                UIntPtr match;
                if (!mem.FindPattern(p.Spec.Pattern, out match)) continue;

                UIntPtr target = MemoryReader.Add(match, p.Spec.PatchOffset);
                int len = p.Spec.Replacement.Length;
                var orig = new byte[len];
                if (!mem.ReadBytes(target, orig)) { Status = "读取原指令失败"; continue; }
                if (!mem.WriteBytes(target, p.Spec.Replacement)) { Status = "写入补丁失败"; continue; }

                p.Applied = true;
                p.Address = target;
                p.Original = orig;
            }

            pending = 0;
            foreach (var p in _patches) if (!p.Applied) pending++;
            Status = pending == 0 ? "已补丁 ✓" : string.Format("已补丁 {0}/{1}", _patches.Length - pending, _patches.Length);
            return pending == 0;
        }

        public override void OnDeactivated(MemoryReader mem)
        {
            if (mem == null || !mem.Attached) { ResetForNewProcess(); return; }
            foreach (var p in _patches)
            {
                if (p.Applied && p.Address != UIntPtr.Zero)
                {
                    // 仅当目标处仍是我们写入的字节时才还原，避免覆盖游戏自己的改动
                    var cur = new byte[p.Original.Length];
                    if (mem.ReadBytes(p.Address, cur) && BytesEqual(cur, p.Spec.Replacement))
                        mem.WriteBytes(p.Address, p.Original);
                }
                p.Applied = false;
                p.Address = UIntPtr.Zero;
                p.Original = null;
            }
            Status = "未激活";
        }

        public override void ResetForNewProcess()
        {
            foreach (var p in _patches)
            {
                p.Applied = false;
                p.Address = UIntPtr.Zero;
                p.Original = null;
            }
            Status = "未激活";
        }

        private static bool BytesEqual(byte[] a, byte[] b)
        {
            if (a.Length != b.Length) return false;
            for (int i = 0; i < a.Length; i++) if (a[i] != b[i]) return false;
            return true;
        }
    }

    /// <summary>
    /// 指针链冻结：每 tick 重新解析“模块基址 + 偏移链”得到字段地址并写回
    /// 目标值（默认在激活瞬间捕获当前值）。逐 tick 重解析可容忍 Mono GC 移动对象。
    /// </summary>
    internal sealed class FreezeFeature : CheatFeature
    {
        public string Module = "";
        public int[] Offsets = new int[0];   // 应用顺序：base+o0 解引用 → +o1 → …
        public bool IsFloat;
        public int IntValue;
        public float FloatValue;
        public bool CaptureOnActivate = true;

        private bool _captured;

        public FreezeFeature(string name, int hotkey, string module, int[] offsets, bool verified)
        {
            Name = name;
            HotkeyNumber = hotkey;
            Module = module;
            Offsets = offsets;
            Verified = verified;
        }

        public override bool Tick(MemoryReader mem)
        {
            UIntPtr moduleBase;
            if (!mem.TryGetModuleBase(Module, out moduleBase)) { Status = "未找到模块 " + Module; return false; }

            UIntPtr final;
            if (!mem.WalkChain(moduleBase, Offsets, out final)) { Status = "指针链解析失败（存档未加载？）"; return false; }

            if (!_captured && CaptureOnActivate)
            {
                if (IsFloat)
                {
                    float f;
                    if (mem.ReadFloat(final, out f)) { FloatValue = f; _captured = true; }
                }
                else
                {
                    int i;
                    if (mem.ReadInt32(final, out i)) { IntValue = i; _captured = true; }
                }
                if (!_captured) { Status = "等待读取当前值…"; return false; }
            }

            bool ok = IsFloat ? mem.WriteFloat(final, FloatValue) : mem.WriteInt32(final, IntValue);
            Status = ok
                ? string.Format("已冻结 ✓（{0}）", IsFloat ? FloatValue.ToString("R") : IntValue.ToString())
                : "写入失败";
            return ok;
        }

        public override void OnDeactivated(MemoryReader mem)
        {
            _captured = false;
            Status = "未激活";
        }

        public override void ResetForNewProcess()
        {
            _captured = false;
            Status = "未激活";
        }
    }

    /// <summary>功能引擎：后台线程周期驱动所有已激活功能，并做进程更换检测。</summary>
    internal sealed class FeatureEngine
    {
        private readonly CheatFeature[] _features;
        private Thread _thread;
        private volatile bool _stop;
        private int _lastPid;

        public MemoryReader Mem;
        public event Action<string> StatusChanged;

        public FeatureEngine(CheatFeature[] features) { _features = features; }

        public void Start()
        {
            if (_thread != null) return;
            _thread = new Thread(Loop) { IsBackground = true, Name = "FeatureEngine" };
            _thread.Start();
        }

        private void Loop()
        {
            int tick = 0;
            while (!_stop)
            {
                tick++;
                if (Mem != null && Mem.Attached)
                {
                    if (Mem.ProcessId != _lastPid)
                    {
                        // 游戏重启：清空各功能的运行期缓存（旧进程里的补丁已随进程消失）
                        _lastPid = Mem.ProcessId;
                        foreach (var f in _features) f.ResetForNewProcess();
                    }

                    int active = 0, failed = 0;
                    foreach (var f in _features)
                    {
                        if (!f.Active) continue;
                        active++;
                        bool ok;
                        try { ok = f.Tick(Mem); }
                        catch (Exception) { ok = false; }
                        if (!ok) failed++;
                    }

                    if (active > 0 && tick % 12 == 0)
                    {
                        var h = StatusChanged;
                        if (h != null)
                        {
                            h(failed == 0
                                ? string.Format("功能运行中：{0} 个激活，全部写入成功", active)
                                : string.Format("功能运行中：{0} 个激活，{1} 个未生效（见功能状态）", active, failed));
                        }
                    }
                }
                Thread.Sleep(40);
            }
        }

        public void SetActive(CheatFeature f, bool on)
        {
            if (on) { f.Active = true; }
            else
            {
                f.Active = false;
                try { f.OnDeactivated(Mem); }
                catch (Exception) { }
            }
        }

        public void DeactivateAll()
        {
            foreach (var f in _features)
                if (f.Active) SetActive(f, false);
        }

        public void Stop() { _stop = true; }
    }
}
