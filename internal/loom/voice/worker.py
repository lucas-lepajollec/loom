# Sherpa 1.13.8 C ABI field declarations derived from the Apache-2.0
# k2-fsa/sherpa-onnx public c-api.h. Keep in sync with the pinned version.
import ctypes as C

class SherpaOnnxFeatureConfig(C.Structure):
    _fields_ = [
        ('sample_rate', C.c_int32),
        ('feature_dim', C.c_int32),
    ]

class SherpaOnnxOnlineTransducerModelConfig(C.Structure):
    _fields_ = [
        ('encoder', C.c_char_p),
        ('decoder', C.c_char_p),
        ('joiner', C.c_char_p),
    ]

class SherpaOnnxOnlineParaformerModelConfig(C.Structure):
    _fields_ = [
        ('encoder', C.c_char_p),
        ('decoder', C.c_char_p),
    ]

class SherpaOnnxOnlineZipformer2CtcModelConfig(C.Structure):
    _fields_ = [
        ('model', C.c_char_p),
    ]

class SherpaOnnxOnlineNemoCtcModelConfig(C.Structure):
    _fields_ = [
        ('model', C.c_char_p),
    ]

class SherpaOnnxOnlineToneCtcModelConfig(C.Structure):
    _fields_ = [
        ('model', C.c_char_p),
    ]

class SherpaOnnxOnlineModelConfig(C.Structure):
    _fields_ = [
        ('transducer', SherpaOnnxOnlineTransducerModelConfig),
        ('paraformer', SherpaOnnxOnlineParaformerModelConfig),
        ('zipformer2_ctc', SherpaOnnxOnlineZipformer2CtcModelConfig),
        ('tokens', C.c_char_p),
        ('num_threads', C.c_int32),
        ('provider', C.c_char_p),
        ('debug', C.c_int32),
        ('model_type', C.c_char_p),
        ('modeling_unit', C.c_char_p),
        ('bpe_vocab', C.c_char_p),
        ('tokens_buf', C.c_char_p),
        ('tokens_buf_size', C.c_int32),
        ('nemo_ctc', SherpaOnnxOnlineNemoCtcModelConfig),
        ('t_one_ctc', SherpaOnnxOnlineToneCtcModelConfig),
    ]

class SherpaOnnxOnlineCtcFstDecoderConfig(C.Structure):
    _fields_ = [
        ('graph', C.c_char_p),
        ('max_active', C.c_int32),
    ]

class SherpaOnnxHomophoneReplacerConfig(C.Structure):
    _fields_ = [
        ('dict_dir', C.c_char_p),
        ('lexicon', C.c_char_p),
        ('rule_fsts', C.c_char_p),
    ]

class SherpaOnnxOnlineRecognizerConfig(C.Structure):
    _fields_ = [
        ('feat_config', SherpaOnnxFeatureConfig),
        ('model_config', SherpaOnnxOnlineModelConfig),
        ('decoding_method', C.c_char_p),
        ('max_active_paths', C.c_int32),
        ('enable_endpoint', C.c_int32),
        ('rule1_min_trailing_silence', C.c_float),
        ('rule2_min_trailing_silence', C.c_float),
        ('rule3_min_utterance_length', C.c_float),
        ('hotwords_file', C.c_char_p),
        ('hotwords_score', C.c_float),
        ('ctc_fst_decoder_config', SherpaOnnxOnlineCtcFstDecoderConfig),
        ('rule_fsts', C.c_char_p),
        ('rule_fars', C.c_char_p),
        ('blank_penalty', C.c_float),
        ('hotwords_buf', C.c_char_p),
        ('hotwords_buf_size', C.c_int32),
        ('hr', SherpaOnnxHomophoneReplacerConfig),
    ]

class SherpaOnnxOfflineTransducerModelConfig(C.Structure):
    _fields_ = [
        ('encoder', C.c_char_p),
        ('decoder', C.c_char_p),
        ('joiner', C.c_char_p),
    ]

class SherpaOnnxOfflineParaformerModelConfig(C.Structure):
    _fields_ = [
        ('model', C.c_char_p),
    ]

class SherpaOnnxOfflineNemoEncDecCtcModelConfig(C.Structure):
    _fields_ = [
        ('model', C.c_char_p),
    ]

class SherpaOnnxOfflineWhisperModelConfig(C.Structure):
    _fields_ = [
        ('encoder', C.c_char_p),
        ('decoder', C.c_char_p),
        ('language', C.c_char_p),
        ('task', C.c_char_p),
        ('tail_paddings', C.c_int32),
        ('enable_token_timestamps', C.c_int32),
        ('enable_segment_timestamps', C.c_int32),
    ]

class SherpaOnnxOfflineTdnnModelConfig(C.Structure):
    _fields_ = [
        ('model', C.c_char_p),
    ]

class SherpaOnnxOfflineSenseVoiceModelConfig(C.Structure):
    _fields_ = [
        ('model', C.c_char_p),
        ('language', C.c_char_p),
        ('use_itn', C.c_int32),
    ]

class SherpaOnnxOfflineMoonshineModelConfig(C.Structure):
    _fields_ = [
        ('preprocessor', C.c_char_p),
        ('encoder', C.c_char_p),
        ('uncached_decoder', C.c_char_p),
        ('cached_decoder', C.c_char_p),
        ('merged_decoder', C.c_char_p),
    ]

class SherpaOnnxOfflineFireRedAsrModelConfig(C.Structure):
    _fields_ = [
        ('encoder', C.c_char_p),
        ('decoder', C.c_char_p),
    ]

class SherpaOnnxOfflineDolphinModelConfig(C.Structure):
    _fields_ = [
        ('model', C.c_char_p),
    ]

class SherpaOnnxOfflineZipformerCtcModelConfig(C.Structure):
    _fields_ = [
        ('model', C.c_char_p),
    ]

class SherpaOnnxOfflineCanaryModelConfig(C.Structure):
    _fields_ = [
        ('encoder', C.c_char_p),
        ('decoder', C.c_char_p),
        ('src_lang', C.c_char_p),
        ('tgt_lang', C.c_char_p),
        ('use_pnc', C.c_int32),
    ]

class SherpaOnnxOfflineWenetCtcModelConfig(C.Structure):
    _fields_ = [
        ('model', C.c_char_p),
    ]

class SherpaOnnxOfflineOmnilingualAsrCtcModelConfig(C.Structure):
    _fields_ = [
        ('model', C.c_char_p),
    ]

class SherpaOnnxOfflineMedAsrCtcModelConfig(C.Structure):
    _fields_ = [
        ('model', C.c_char_p),
    ]

class SherpaOnnxOfflineFunASRNanoModelConfig(C.Structure):
    _fields_ = [
        ('encoder_adaptor', C.c_char_p),
        ('llm', C.c_char_p),
        ('embedding', C.c_char_p),
        ('tokenizer', C.c_char_p),
        ('system_prompt', C.c_char_p),
        ('user_prompt', C.c_char_p),
        ('max_new_tokens', C.c_int32),
        ('temperature', C.c_float),
        ('top_p', C.c_float),
        ('seed', C.c_int32),
        ('language', C.c_char_p),
        ('itn', C.c_int32),
        ('hotwords', C.c_char_p),
    ]

class SherpaOnnxOfflineFireRedAsrCtcModelConfig(C.Structure):
    _fields_ = [
        ('model', C.c_char_p),
    ]

class SherpaOnnxOfflineQwen3ASRModelConfig(C.Structure):
    _fields_ = [
        ('conv_frontend', C.c_char_p),
        ('encoder', C.c_char_p),
        ('decoder', C.c_char_p),
        ('tokenizer', C.c_char_p),
        ('max_total_len', C.c_int32),
        ('max_new_tokens', C.c_int32),
        ('temperature', C.c_float),
        ('top_p', C.c_float),
        ('seed', C.c_int32),
        ('hotwords', C.c_char_p),
    ]

class SherpaOnnxOfflineCohereTranscribeModelConfig(C.Structure):
    _fields_ = [
        ('encoder', C.c_char_p),
        ('decoder', C.c_char_p),
        ('language', C.c_char_p),
        ('use_punct', C.c_int32),
        ('use_itn', C.c_int32),
    ]

class SherpaOnnxOfflineModelConfig(C.Structure):
    _fields_ = [
        ('transducer', SherpaOnnxOfflineTransducerModelConfig),
        ('paraformer', SherpaOnnxOfflineParaformerModelConfig),
        ('nemo_ctc', SherpaOnnxOfflineNemoEncDecCtcModelConfig),
        ('whisper', SherpaOnnxOfflineWhisperModelConfig),
        ('tdnn', SherpaOnnxOfflineTdnnModelConfig),
        ('tokens', C.c_char_p),
        ('num_threads', C.c_int32),
        ('debug', C.c_int32),
        ('provider', C.c_char_p),
        ('model_type', C.c_char_p),
        ('modeling_unit', C.c_char_p),
        ('bpe_vocab', C.c_char_p),
        ('telespeech_ctc', C.c_char_p),
        ('sense_voice', SherpaOnnxOfflineSenseVoiceModelConfig),
        ('moonshine', SherpaOnnxOfflineMoonshineModelConfig),
        ('fire_red_asr', SherpaOnnxOfflineFireRedAsrModelConfig),
        ('dolphin', SherpaOnnxOfflineDolphinModelConfig),
        ('zipformer_ctc', SherpaOnnxOfflineZipformerCtcModelConfig),
        ('canary', SherpaOnnxOfflineCanaryModelConfig),
        ('wenet_ctc', SherpaOnnxOfflineWenetCtcModelConfig),
        ('omnilingual', SherpaOnnxOfflineOmnilingualAsrCtcModelConfig),
        ('medasr', SherpaOnnxOfflineMedAsrCtcModelConfig),
        ('funasr_nano', SherpaOnnxOfflineFunASRNanoModelConfig),
        ('fire_red_asr_ctc', SherpaOnnxOfflineFireRedAsrCtcModelConfig),
        ('qwen3_asr', SherpaOnnxOfflineQwen3ASRModelConfig),
        ('cohere_transcribe', SherpaOnnxOfflineCohereTranscribeModelConfig),
    ]

class SherpaOnnxOfflineLMConfig(C.Structure):
    _fields_ = [
        ('model', C.c_char_p),
        ('scale', C.c_float),
    ]

class SherpaOnnxOfflineRecognizerConfig(C.Structure):
    _fields_ = [
        ('feat_config', SherpaOnnxFeatureConfig),
        ('model_config', SherpaOnnxOfflineModelConfig),
        ('lm_config', SherpaOnnxOfflineLMConfig),
        ('decoding_method', C.c_char_p),
        ('max_active_paths', C.c_int32),
        ('hotwords_file', C.c_char_p),
        ('hotwords_score', C.c_float),
        ('rule_fsts', C.c_char_p),
        ('rule_fars', C.c_char_p),
        ('blank_penalty', C.c_float),
        ('hr', SherpaOnnxHomophoneReplacerConfig),
    ]

class SherpaOnnxOfflineTtsVitsModelConfig(C.Structure):
    _fields_ = [
        ('model', C.c_char_p),
        ('lexicon', C.c_char_p),
        ('tokens', C.c_char_p),
        ('data_dir', C.c_char_p),
        ('noise_scale', C.c_float),
        ('noise_scale_w', C.c_float),
        ('length_scale', C.c_float),
        ('dict_dir', C.c_char_p),
    ]

class SherpaOnnxOfflineTtsMatchaModelConfig(C.Structure):
    _fields_ = [
        ('acoustic_model', C.c_char_p),
        ('vocoder', C.c_char_p),
        ('lexicon', C.c_char_p),
        ('tokens', C.c_char_p),
        ('data_dir', C.c_char_p),
        ('noise_scale', C.c_float),
        ('length_scale', C.c_float),
        ('dict_dir', C.c_char_p),
    ]

class SherpaOnnxOfflineTtsKokoroModelConfig(C.Structure):
    _fields_ = [
        ('model', C.c_char_p),
        ('voices', C.c_char_p),
        ('tokens', C.c_char_p),
        ('data_dir', C.c_char_p),
        ('length_scale', C.c_float),
        ('dict_dir', C.c_char_p),
        ('lexicon', C.c_char_p),
        ('lang', C.c_char_p),
    ]

class SherpaOnnxOfflineTtsKittenModelConfig(C.Structure):
    _fields_ = [
        ('model', C.c_char_p),
        ('voices', C.c_char_p),
        ('tokens', C.c_char_p),
        ('data_dir', C.c_char_p),
        ('length_scale', C.c_float),
    ]

class SherpaOnnxOfflineTtsZipvoiceModelConfig(C.Structure):
    _fields_ = [
        ('tokens', C.c_char_p),
        ('encoder', C.c_char_p),
        ('decoder', C.c_char_p),
        ('vocoder', C.c_char_p),
        ('data_dir', C.c_char_p),
        ('lexicon', C.c_char_p),
        ('feat_scale', C.c_float),
        ('t_shift', C.c_float),
        ('target_rms', C.c_float),
        ('guidance_scale', C.c_float),
    ]

class SherpaOnnxOfflineTtsPocketModelConfig(C.Structure):
    _fields_ = [
        ('lm_flow', C.c_char_p),
        ('lm_main', C.c_char_p),
        ('encoder', C.c_char_p),
        ('decoder', C.c_char_p),
        ('text_conditioner', C.c_char_p),
        ('vocab_json', C.c_char_p),
        ('token_scores_json', C.c_char_p),
        ('voice_embedding_cache_capacity', C.c_int32),
    ]

class SherpaOnnxOfflineTtsSupertonicModelConfig(C.Structure):
    _fields_ = [
        ('duration_predictor', C.c_char_p),
        ('text_encoder', C.c_char_p),
        ('vector_estimator', C.c_char_p),
        ('vocoder', C.c_char_p),
        ('tts_json', C.c_char_p),
        ('unicode_indexer', C.c_char_p),
        ('voice_style', C.c_char_p),
    ]

class SherpaOnnxOfflineTtsModelConfig(C.Structure):
    _fields_ = [
        ('vits', SherpaOnnxOfflineTtsVitsModelConfig),
        ('num_threads', C.c_int32),
        ('debug', C.c_int32),
        ('provider', C.c_char_p),
        ('matcha', SherpaOnnxOfflineTtsMatchaModelConfig),
        ('kokoro', SherpaOnnxOfflineTtsKokoroModelConfig),
        ('kitten', SherpaOnnxOfflineTtsKittenModelConfig),
        ('zipvoice', SherpaOnnxOfflineTtsZipvoiceModelConfig),
        ('pocket', SherpaOnnxOfflineTtsPocketModelConfig),
        ('supertonic', SherpaOnnxOfflineTtsSupertonicModelConfig),
    ]

class SherpaOnnxOfflineTtsConfig(C.Structure):
    _fields_ = [
        ('model', SherpaOnnxOfflineTtsModelConfig),
        ('rule_fsts', C.c_char_p),
        ('max_num_sentences', C.c_int32),
        ('rule_fars', C.c_char_p),
        ('silence_scale', C.c_float),
    ]

class SherpaOnnxSileroVadModelConfig(C.Structure):
    _fields_ = [
        ('model', C.c_char_p),
        ('threshold', C.c_float),
        ('min_silence_duration', C.c_float),
        ('min_speech_duration', C.c_float),
        ('window_size', C.c_int32),
        ('max_speech_duration', C.c_float),
    ]

class SherpaOnnxTenVadModelConfig(C.Structure):
    _fields_ = [
        ('model', C.c_char_p),
        ('threshold', C.c_float),
        ('min_silence_duration', C.c_float),
        ('min_speech_duration', C.c_float),
        ('window_size', C.c_int32),
        ('max_speech_duration', C.c_float),
    ]

class SherpaOnnxVadModelConfig(C.Structure):
    _fields_ = [
        ('silero_vad', SherpaOnnxSileroVadModelConfig),
        ('sample_rate', C.c_int32),
        ('num_threads', C.c_int32),
        ('provider', C.c_char_p),
        ('debug', C.c_int32),
        ('ten_vad', SherpaOnnxTenVadModelConfig),
    ]

class SherpaOnnxGeneratedAudio(C.Structure):
    _fields_ = [
        ('samples', C.POINTER(C.c_float)),
        ('n', C.c_int32),
        ('sample_rate', C.c_int32),
    ]

class SherpaOnnxSpeechSegment(C.Structure):
    _fields_ = [
        ('start', C.c_int32),
        ('samples', C.POINTER(C.c_float)),
        ('n', C.c_int32),
    ]

class SherpaOnnxGenerationConfig(C.Structure):
    _fields_ = [
        ('silence_scale', C.c_float),
        ('speed', C.c_float),
        ('sid', C.c_int32),
        ('reference_audio', C.POINTER(C.c_float)),
        ('reference_audio_len', C.c_int32),
        ('reference_sample_rate', C.c_int32),
        ('reference_text', C.c_char_p),
        ('num_steps', C.c_int32),
        ('extra', C.c_char_p),
    ]

# The worker opens no sockets. Go owns authentication, bounds and lifecycle.
import array
import base64
import json
import sys
import os

def emit(value):
    print(json.dumps(value, ensure_ascii=False), flush=True)

def bind(lib, name, result, *args):
    fn = getattr(lib, 'SherpaOnnx' + name)
    fn.restype, fn.argtypes = result, list(args)
    return fn

class Engine:
    def __init__(self, library, mode, config):
        self.dll_dir = os.add_dll_directory(os.path.dirname(library)) if sys.platform == "win32" else None
        self.lib = C.CDLL(library)
        self.mode, self.config, self.streams = mode, config, {}
        self.api = {}
        P, I, F = C.c_void_p, C.c_int32, C.c_float
        spec = {
            'GetVersionStr': (C.c_char_p, []),
            'CreateOnlineRecognizer': (P, [C.POINTER(SherpaOnnxOnlineRecognizerConfig)]),
            'CreateOfflineRecognizer': (P, [C.POINTER(SherpaOnnxOfflineRecognizerConfig)]),
            'CreateOfflineTts': (P, [C.POINTER(SherpaOnnxOfflineTtsConfig)]),
            'OfflineTtsNumSpeakers': (I, [P]),
            'OfflineTtsSampleRate': (I, [P]),
            'OfflineTtsGenerateWithConfig': (C.POINTER(SherpaOnnxGeneratedAudio), [P, C.c_char_p, C.POINTER(SherpaOnnxGenerationConfig), P, P]),
            'DestroyOfflineTtsGeneratedAudio': (None, [C.POINTER(SherpaOnnxGeneratedAudio)]),
            'CreateOnlineStream': (P, [P]), 'CreateOfflineStream': (P, [P]),
            'DestroyOnlineStream': (None, [P]), 'DestroyOfflineStream': (None, [P]),
            'OnlineStreamAcceptWaveform': (None, [P, I, C.POINTER(F), I]),
            'AcceptWaveformOffline': (None, [P, I, C.POINTER(F), I]),
            'IsOnlineStreamReady': (I, [P, P]), 'DecodeOnlineStream': (None, [P, P]),
            'DecodeOfflineStream': (None, [P, P]), 'OnlineStreamInputFinished': (None, [P]),
            'OnlineStreamIsEndpoint': (I, [P, P]), 'OnlineStreamReset': (None, [P, P]),
            'GetOnlineStreamResultAsJson': (P, [P, P]), 'DestroyOnlineStreamResultJson': (None, [P]),
            'GetOfflineStreamResultAsJson': (P, [P]), 'DestroyOfflineStreamResultJson': (None, [P]),
            'CreateVoiceActivityDetector': (P, [C.POINTER(SherpaOnnxVadModelConfig), F]),
            'DestroyVoiceActivityDetector': (None, [P]),
            'VoiceActivityDetectorAcceptWaveform': (None, [P, C.POINTER(F), I]),
            'VoiceActivityDetectorEmpty': (I, [P]), 'VoiceActivityDetectorFlush': (None, [P]),
            'VoiceActivityDetectorFront': (C.POINTER(SherpaOnnxSpeechSegment), [P]),
            'VoiceActivityDetectorPop': (None, [P]),
            'DestroySpeechSegment': (None, [C.POINTER(SherpaOnnxSpeechSegment)]),
        }
        for name, (result, args) in spec.items():
            self.api[name] = bind(self.lib, name, result, *args)
        if self.api['GetVersionStr']().decode() != '1.13.8':
            raise ValueError('worker requires sherpa 1.13.8 ABI')
        self.initialize()

    def initialize(self):
        c = self.config
        model = c['model']
        if self.mode == 'tts':
            cfg = SherpaOnnxOfflineTtsConfig()
            cfg.model.num_threads = c['threads']
            # TTS CPU avoids competing with the LLM GPU and is supported by
            # Piper/Kokoro int8; the opted-in CUDA provider belongs to STT.
            cfg.model.provider = b'cpu'
            cfg.max_num_sentences = 1
            target = getattr(cfg.model, model['family'])
            for key, value in model['files'].items():
                setattr(target, key, value.encode())
            target.length_scale = 1.0
            if model['family'] == 'vits':
                target.noise_scale, target.noise_scale_w = 0.667, 0.8
            else:
                target.lang = c['language'].encode()
            self.engine = self.api['CreateOfflineTts'](C.byref(cfg))
        else:
            self.online = model['family'] == 'online-transducer'
            cfg = SherpaOnnxOnlineRecognizerConfig() if self.online else SherpaOnnxOfflineRecognizerConfig()
            cfg.feat_config.sample_rate, cfg.feat_config.feature_dim = 16000, 80
            cfg.decoding_method, cfg.max_active_paths = b'greedy_search', 4
            cfg.model_config.tokens = model['files']['tokens'].encode()
            cfg.model_config.num_threads = c['threads']
            cfg.model_config.provider = c['provider'].encode()
            if model['family'] == 'offline-nemo-ctc':
                cfg.model_config.nemo_ctc.model = model['files']['model'].encode()
            else:
                for key in ['encoder', 'decoder', 'joiner']:
                    setattr(cfg.model_config.transducer, key, model['files'][key].encode())
                cfg.model_config.model_type = b'zipformer2' if self.online else b'nemo_transducer'
            if self.online:
                cfg.enable_endpoint = 1
                cfg.rule1_min_trailing_silence = c['endpoint_silence'] * 2
                cfg.rule2_min_trailing_silence = c['endpoint_silence']
                cfg.rule3_min_utterance_length = 30
            self.engine = self.api['CreateOnlineRecognizer' if self.online else 'CreateOfflineRecognizer'](C.byref(cfg))
        if not self.engine:
            raise ValueError('sherpa rejected model configuration')

    def floats(self, pcm):
        values = array.array('h', base64.b64decode(pcm, validate=True))
        if sys.byteorder != 'little':
            values.byteswap()
        return (C.c_float * len(values))(*(x / 32768.0 for x in values))

    def result(self, stream, online):
        ptr = self.api['GetOnlineStreamResultAsJson'](self.engine, stream) if online else self.api['GetOfflineStreamResultAsJson'](stream)
        if not ptr:
            raise ValueError('sherpa returned no transcript')
        try:
            return json.loads(C.string_at(ptr))['text']
        finally:
            self.api['DestroyOnlineStreamResultJson' if online else 'DestroyOfflineStreamResultJson'](ptr)

    def new_vad(self):
        c = self.config
        cfg = SherpaOnnxVadModelConfig()
        cfg.sample_rate, cfg.num_threads, cfg.provider = 16000, 1, b'cpu'
        cfg.silero_vad.model = c['vad_model'].encode()
        cfg.silero_vad.threshold = c['vad_threshold']
        cfg.silero_vad.min_silence_duration = c['endpoint_silence']
        cfg.silero_vad.min_speech_duration = c['min_speech']
        cfg.silero_vad.max_speech_duration, cfg.silero_vad.window_size = 30, 512
        vad = self.api['CreateVoiceActivityDetector'](C.byref(cfg), 60.0)
        if not vad:
            raise ValueError('sherpa rejected VAD configuration')
        return vad

    def close_stream(self, key):
        stream = self.streams.pop(key, None)
        if stream:
            self.api['DestroyOnlineStream' if self.online else 'DestroyVoiceActivityDetector'](stream)

    def request(self, req):
        op = req['op']
        if op == 'health':
            return
        if op == 'tts':
            sid = req['voice']
            if sid < 0 or sid >= self.api['OfflineTtsNumSpeakers'](self.engine):
                raise ValueError('voice id outside installed model speaker range')
            emit({'sample_rate': self.api['OfflineTtsSampleRate'](self.engine)})
            cfg = SherpaOnnxGenerationConfig()
            cfg.sid, cfg.speed, cfg.silence_scale = sid, req['speed'], 0.2
            callback_type = C.CFUNCTYPE(C.c_int32, C.POINTER(C.c_float), C.c_int32, C.c_float, C.c_void_p)
            def callback(samples, n, progress, arg):
                pcm = array.array('h', (max(-32768, min(32767, int(samples[i] * 32767))) for i in range(n)))
                if sys.byteorder != 'little':
                    pcm.byteswap()
                emit({'audio': base64.b64encode(pcm.tobytes()).decode()})
                return 1
            cb = callback_type(callback)
            audio = self.api['OfflineTtsGenerateWithConfig'](self.engine, req['text'].encode(), C.byref(cfg), C.cast(cb, C.c_void_p), None)
            if not audio:
                raise ValueError('sherpa returned no audio')
            emit({'sample_rate': audio.contents.sample_rate, 'samples': audio.contents.n})
            self.api['DestroyOfflineTtsGeneratedAudio'](audio)
            return
        key = req['session']
        if op == 'close':
            self.close_stream(key)
            return
        if key not in self.streams:
            if len(self.streams) >= 8:
                raise ValueError('voice session limit reached')
            self.streams[key] = self.api['CreateOnlineStream'](self.engine) if self.online else self.new_vad()
            if not self.streams[key]:
                raise ValueError('sherpa could not create stream')
        stream = self.streams[key]
        samples = self.floats(req.get('pcm', ''))
        finish = op == 'finish'
        if self.online:
            self.api['OnlineStreamAcceptWaveform'](stream, 16000, samples, len(samples))
            if finish:
                self.api['OnlineStreamAcceptWaveform'](stream, 16000, (C.c_float * 8000)(), 8000)
                self.api['OnlineStreamInputFinished'](stream)
            while self.api['IsOnlineStreamReady'](self.engine, stream):
                self.api['DecodeOnlineStream'](self.engine, stream)
            final = finish or bool(self.api['OnlineStreamIsEndpoint'](self.engine, stream))
            emit({'type': 'final' if final else 'partial', 'text': self.result(stream, True)})
            if finish:
                self.close_stream(key)
            elif final:
                self.api['OnlineStreamReset'](self.engine, stream)
        else:
            self.api['VoiceActivityDetectorAcceptWaveform'](stream, samples, len(samples))
            if finish:
                self.api['VoiceActivityDetectorFlush'](stream)
            while not self.api['VoiceActivityDetectorEmpty'](stream):
                segment = self.api['VoiceActivityDetectorFront'](stream)
                offline = self.api['CreateOfflineStream'](self.engine)
                if not segment or not offline:
                    raise ValueError('sherpa could not create offline stream')
                try:
                    self.api['AcceptWaveformOffline'](offline, 16000, segment.contents.samples, segment.contents.n)
                    self.api['DecodeOfflineStream'](self.engine, offline)
                    emit({'type': 'final', 'text': self.result(offline, False)})
                finally:
                    self.api['DestroyOfflineStream'](offline)
                    self.api['DestroySpeechSegment'](segment)
                    self.api['VoiceActivityDetectorPop'](stream)
            if finish:
                self.close_stream(key)

if __name__ == '__main__':
    try:
        engine = Engine(sys.argv[1], sys.argv[2], json.loads(sys.stdin.readline()))
        emit({'ready': True})
        for line in sys.stdin:
            try:
                engine.request(json.loads(line))
                emit({'done': True})
            except Exception as e:
                emit({'error': str(e), 'done': True})
    except Exception as e:
        emit({'error': str(e), 'done': True})
        sys.exit(1)
