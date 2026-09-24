using System;
using System.Reflection;
using System.Threading;

namespace OriTrainerDEDLL.Features
{
    // 死亡数归零：持续把 SeinDeathCounter.m_deathCounter 保持为 0。
    //
    // ---- 为什么不写 SeinDeathCounter.Count ----
    // 游戏提供了 public static 的 Count 属性，看起来一行就能搞定：
    //     SeinDeathCounter.Count = 0;
    // 但它的 setter 带了额外动作（IL）：
    //     if (Instance != null) {
    //         Instance.m_deathCounter = value;
    //         SaveSceneManager.Master.Save(Checkpoint.SaveGameData.Master);   // ← 白送的
    //     }
    // SaveSceneManager.Save(SaveScene) 会遍历存档里所有 ISerializable 逐个 Serialize()，
    // 重建整个 SaveScene.SaveObjects 内存缓冲。本功能要持续写入（见下），若走属性就是
    // 每 10ms 一次全量场景序列化，既拖垮游戏，也会在存档流程中间插入非预期的缓冲重算。
    //
    // 因此改为反射直写私有字段 m_deathCounter（private、非 readonly、非 static，
    // 可写）。这在语义上等同于 golang 版的裸内存写入 nav+0x14 —— 那边之所以没有
    // 副作用，正是因为它绕过了 setter；这里用元数据取得同一效果，不必自己算偏移。
    //
    // ---- 为什么必须持续写入 ----
    // SeinDamageReciever.OnKill 是唯一的自增点：
    //     SeinDeathCounter.Count = SeinDeathCounter.Count + 1;
    // 它是"读-加-写"，不是"置位"，所以写一次 0 挡不住下一次死亡。只有持续保持 0
    // 才能在整局游戏中都不累积。这与 InfiniteSkillPoints 的一次性写入不同：那个是
    // 落盘即定局，这个是必须持续压制的运行时状态。
    //
    // ---- 为什么不需要主线程钩子 ----
    // 这条链上没有任何 Unity native 调用（逐层核过 IL）：
    //     get_Count → ldsfld Instance / Object::op_Equality / ldfld m_deathCounter
    //     Object::op_Equality → CompareBaseObjects → IsNativeObjectAlive → GetCachedPtr
    // 全部是托管 IL，所以与 UnlimitedLife / UnlimitedEnergy 一样用定时器即可，
    // 不像 InfiniteDash / InfiniteDoubleJump / ShowMap 那样必须挂 OnGameFixedUpdate。
    //
    // ---- 停止不还原 ----
    // Stop() 只停定时器，不回写原值：m_deathCounter 是存档字段（SeinDeathCounter
    // 继承 SaveSerialize，Serialize 里就是 ar.Serialize(ref m_deathCounter)），
    // 游戏自然存档时 0 就已经落盘，还原只会给出"能收回来"的假象。
    // 停止后死亡数从当前值（0）继续正常累加。
    //
    // ---- 已知连带效果 ----
    //   · 成就：Act 3 结局时 AchievementsLogic 判定 get_Count() == 0 会授予
    //     NoDeathsAchievementAsset（"全程未死亡"）。这是本功能的预期用途。
    //   · 排行榜 / Steam 遥测：LeaderboardsController.UploadScores 与
    //     SeinDeathCounter.SendTelemetryData 读的也是这个字段，会读到 0。
    public static class ZeroDeaths
    {
        private const int IntervalMs = 10;

        private static readonly BindingFlags Private =
            BindingFlags.NonPublic | BindingFlags.Instance;

        private static FieldInfo _fCount; // m_deathCounter
        private static Timer _timer;

        public static void Start()
        {
            if (_timer != null) return; // 幂等：重复 Start 不重复起定时器

            // 字段名对不上就直接失败（Loader 会记进错误日志），而不是每 10ms 静默空转
            _fCount = typeof(SeinDeathCounter).GetField("m_deathCounter", Private) ?? throw new Exception("SeinDeathCounter 的字段名与预期不符，功能无法工作");

            _timer = new Timer(Tick, null, 0, IntervalMs);
        }

        public static void Stop()
        {
            if (_timer == null) return;

            _timer.Dispose();
            _timer = null;
        }

        private static void Tick(object state)
        {
            // 定时器回调里的未捕获异常会终止整个进程（即游戏），必须自己兜住
            try
            {
                // 未进游戏、或对象已销毁时为 null。SeinDeathCounter 是 MonoBehaviour，
                // 这里的 != null 走 UnityEngine.Object::op_Equality，假空（原生对象已销毁
                // 但托管引用还在）同样会被判为 null，不必自己判 m_CachedPtr。
                SeinDeathCounter counter = SeinDeathCounter.Instance;
                if (counter == null) return;

                // 走 public 的 Count 读（无副作用），走反射写（绕开 setter 的存档动作）。
                // 正常游玩时写入次数 ≈ 死亡次数，而不是每秒 100 次。
                if (SeinDeathCounter.Count > 0)
                    _fCount.SetValue(counter, 0);
            }
            catch { }
        }
    }
}
