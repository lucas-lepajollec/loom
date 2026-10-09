# Executed with the embedded worker by TestVoiceWorkerNativeConfigAndCallback.
# No models or native library: observe the actual ctypes structs/API callback.
for threads in (4, 6):
    for family in ('vits', 'kokoro', 'online-transducer', 'offline-nemo-ctc'):
        engine = Engine.__new__(Engine)
        engine.mode = 'tts' if family in ('vits', 'kokoro') else 'stt'
        engine.config = {'threads': threads, 'provider': 'cpu', 'language': 'fr',
                         'endpoint_silence': .7, 'model': {'family': family,
                         'files': {'model': 'model.onnx'} if family in ('vits', 'kokoro', 'offline-nemo-ctc') else
                                  {'encoder': 'encoder.onnx', 'decoder': 'decoder.onnx', 'joiner': 'joiner.onnx'}}}
        if engine.mode == 'stt':
            engine.config['model']['files']['tokens'] = 'tokens.txt'
        observed = []
        def create(pointer):
            config_type = {'tts': SherpaOnnxOfflineTtsConfig,
                           'online-transducer': SherpaOnnxOnlineRecognizerConfig,
                           'offline-nemo-ctc': SherpaOnnxOfflineRecognizerConfig}['tts' if engine.mode == 'tts' else family]
            config = C.cast(pointer, C.POINTER(config_type)).contents
            model = config.model if engine.mode == 'tts' else config.model_config
            assert model.num_threads == threads, (family, model.num_threads, threads)
            if engine.mode == 'tts':
                assert config.max_num_sentences == 1
                assert model.provider == b'cpu'
            observed.append(True)
            return 1
        engine.api = {'CreateOfflineTts': create, 'CreateOnlineRecognizer': create, 'CreateOfflineRecognizer': create}
        engine.initialize()
        assert observed == [True]

engine = Engine.__new__(Engine)
engine.engine = 1
values = (C.c_float * 6401)(*([.5] * 6401))
audio = SherpaOnnxGeneratedAudio(values, 6401, 24000)
events, freed = [], []
generating = False

def capture(event):
    if 'audio' in event:
        assert generating, 'audio was buffered until generation returned'
    events.append(event)

def generate(handle, text, config, callback, arg):
    global generating
    cfg = C.cast(config, C.POINTER(SherpaOnnxGenerationConfig)).contents
    assert cfg.sid == 0 and abs(cfg.speed - 1.25) < .001
    cb_type = C.CFUNCTYPE(C.c_int32, C.POINTER(C.c_float), C.c_int32, C.c_float, C.c_void_p)
    cb = C.cast(callback, cb_type)
    generating = True
    assert cb(values, len(values), 1, None) == 1
    assert len([e for e in events if 'audio' in e]) == 3
    generating = False
    return C.pointer(audio)

engine.api = {'OfflineTtsNumSpeakers': lambda _: 1, 'OfflineTtsSampleRate': lambda _: 24000,
              'OfflineTtsGenerateWithConfig': generate,
              'DestroyOfflineTtsGeneratedAudio': lambda _: freed.append(True)}
emit = capture
engine.request({'op': 'tts', 'text': 'Two sentences. Second sentence.', 'voice': 0, 'speed': 1.25})
assert events[0] == {'sample_rate': 24000}
chunks = [base64.b64decode(e['audio']) for e in events if 'audio' in e]
assert [len(c) for c in chunks] == [6400, 6400, 2]
assert sum(map(len, chunks)) == len(values) * 2
assert events[-1] == {'sample_rate': 24000, 'samples': 6401}
assert freed == [True]

# A failed writer must stop the callback and propagate its exception after
# releasing the native audio; ctypes must not swallow it and continue.
def fail_emit(event):
    if 'audio' in event:
        raise BrokenPipeError('output closed')

def generate_failure(handle, text, config, callback, arg):
    cb_type = C.CFUNCTYPE(C.c_int32, C.POINTER(C.c_float), C.c_int32, C.c_float, C.c_void_p)
    assert C.cast(callback, cb_type)(values, len(values), 1, None) == 0
    return C.pointer(audio)

emit = fail_emit
engine.api['OfflineTtsGenerateWithConfig'] = generate_failure
try:
    engine.request({'op': 'tts', 'text': 'Failure.', 'voice': 0, 'speed': 1.25})
    raise AssertionError('callback error swallowed')
except BrokenPipeError:
    pass
assert freed == [True, True]
