using System;

namespace OriTrainerDEDLL.Features
{
    // 无限能力点数：一次性把能力点写满，不做持续写入。
    //
    // 与其余功能刻意不同的设计：
    //   能力点的唯一用途就是在技能树里买技能，而买技能时
    //   SkillTreeManager.OnMenuItemPressed 尾部会立刻 CreateCheckpoint() + PerformSave()，
    //   且 SeinLevel.Serialize 会把 Current / Experience / SkillPoints / HasSpentSkillPoint
    //   全部写进存档。也就是说：开启本功能后只要买过一次技能，点数就已经落盘、无法回收。
    //   既然落盘已成事实，"停止功能"没有意义 —— Stop() 因此是空实现，
    //   本功能也没有定时器（不是需要每帧维持的运行时状态，而是写入即定局的状态）。
    //
    //   需要再次写满时重新发送 Start 即可（重复调用是幂等的）。
    //
    // 不需要反射，也不需要主线程钩子：
    //   SeinCharacter.Level (public SeinLevel) -> SeinLevel.SkillPoints / Current (public Int32)
    //   三者都是 public 字段。SeinLevel 不是 prefab 组件，随角色创建即存在，
    //   所以不像冲刺/二段跳那样需要 EnsureRightPrefabsAreThereForAbilities()。
    //
    // 等级修正：技能树开启条件是 SeinSoulFlame.AllowedToAccessSkillTree
    //     m_sein.Level.Current > 0 && IsSafeToCastSoulFlame == Safe
    // 而 Current 只在 SeinLevel.LevelUp() 里 ++（由经验值累积触发），
    // 全新存档还没升过级时它就是 0 —— 技能树打不开，能力点花不出去。
    // 这里只在 <= 0 时改写为 1，不影响正常存档的等级。
    public static class InfiniteSkillPoints
    {
        private const int TargetSkillPoints = 99; // 一次性写99点技能点

        public static void Start()
        {
            SeinCharacter sein = Game.Characters.Sein ?? throw new Exception("当前不在游戏中（Sein 为空），无法写入能力点");

            SeinLevel level = sein.Level ?? throw new Exception("SeinLevel 为空，无法写入能力点");

            // 技能树开启条件含 Current > 0，只在尚未升级时修正

            if (level.Current <= 0)
                level.Current = 1;

            level.SkillPoints = TargetSkillPoints;
        }

        // 刻意留空：点数是已经写进存档的状态，不是需要维持的运行时状态，
        // 停止功能既不能也不应该把它收回（见文件头说明）。
        public static void Stop()
        {
        }
    }
}
