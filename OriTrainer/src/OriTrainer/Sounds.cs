/*
    功能开关音效：按对应键时播一声短提示音。
    音频资源（on.wav / off.wav）作为 EmbeddedResource 嵌进本 exe，见 OriTrainer.csproj。

    ⚠ 本文件**只负责播放音效** —— 音效只是 "按下按键" 这一动作的反馈。

    用 System.Media.SoundPlayer（net48 现成，不用手写 P/Invoke）：
      · WAV 资源直接内嵌，"从流播放"无需落临时文件
      · Play() 非阻塞异步（内部起线程调 PlaySound），不会卡主循环
      · 11025Hz/16bit/单声道 PCM，SoundPlayer 完全支持
*/
using System.IO;
using System.Media;
using System.Reflection;

namespace OriTrainer
{
    internal static class Sounds
    {
        // 内嵌资源名，与 OriTrainer.csproj 里 EmbeddedResource 的 LogicalName 一致。
        private const string ResOn = "OriTrainer.Sounds.On";
        private const string ResOff = "OriTrainer.Sounds.Off";

        // 播放音频句柄
        private static SoundPlayer _on;
        private static SoundPlayer _off;

        // 启动时调用一次，把两个音效从内嵌资源解出并预加载。
        // 必须在任何 Play 之前调用，否则 Play 对 null 静默跳过。
        public static void Initialize()
        {
            if (_on != null) return; // 幂等：已初始化过则跳过，不重复加载
            Assembly asm = Assembly.GetExecutingAssembly();
            _on = Load(asm, ResOn);
            _off = Load(asm, ResOff);
        }

        // 从内嵌资源建 SoundPlayer 并预加载。Stream 为 null（资源缺失）时返回 null，
        // 后续 Play 对 null 直接跳过，不抛。
        private static SoundPlayer Load(Assembly asm, string resName)
        {
            Stream s = asm.GetManifestResourceStream(resName);
            if (s == null) return null;

            var p = new SoundPlayer(s);
            try { p.Load(); } catch { return null; } // 预加载失败按无音效处理
            return p;
        }

        public static void PlayOn()
        {
            if (_on != null) { try { _on.Play(); } catch { } }
        }

        public static void PlayOff()
        {
            if (_off != null) { try { _off.Play(); } catch { } }
        }
    }
}